package acme

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStorage(t *testing.T) (*redisStorage, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	s := newRedisStorageClient(client, "")
	t.Cleanup(func() { _ = s.Close() })

	return s, mr
}

func TestRedisStorage(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStorage(t)

	if _, err := s.Load(ctx, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("load missing: %v", err)
	}

	for _, k := range []string{
		"certificates/acme/example.com/example.com.crt",
		"certificates/acme/example.com/example.com.key",
		"certificates/acme/other.com/other.com.crt",
		"acme/account.json",
	} {
		if err := s.Store(ctx, k, []byte("v:"+k)); err != nil {
			t.Fatal(err)
		}
	}

	v, err := s.Load(ctx, "acme/account.json")
	if err != nil || string(v) != "v:acme/account.json" {
		t.Fatalf("load = %q, %v", v, err)
	}

	if !s.Exists(ctx, "certificates/acme") || !s.Exists(ctx, "acme/account.json") || s.Exists(ctx, "certificates/x") {
		t.Fatal("exists is wrong")
	}

	list, err := s.List(ctx, "certificates/acme", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"certificates/acme/example.com", "certificates/acme/other.com"}; !slices.Equal(list, want) {
		t.Fatalf("list = %v, want %v", list, want)
	}

	list, err = s.List(ctx, "certificates", true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(list, "certificates/acme") || !slices.Contains(list, "certificates/acme/example.com/example.com.key") {
		t.Fatalf("recursive list = %v", list)
	}

	if _, err := s.List(ctx, "nothing", true); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("list missing: %v", err)
	}

	info, err := s.Stat(ctx, "acme/account.json")
	if err != nil || !info.IsTerminal || info.Size == 0 || info.Modified.IsZero() {
		t.Fatalf("stat file = %+v, %v", info, err)
	}

	info, err = s.Stat(ctx, "certificates/acme")
	if err != nil || info.IsTerminal || info.Modified.IsZero() {
		t.Fatalf("stat dir = %+v, %v", info, err)
	}

	if _, err := s.Stat(ctx, "nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat missing: %v", err)
	}

	if err := s.Delete(ctx, "certificates/acme/example.com"); err != nil {
		t.Fatal(err)
	}
	if s.Exists(ctx, "certificates/acme/example.com/example.com.crt") || !s.Exists(ctx, "certificates/acme/other.com/other.com.crt") {
		t.Fatal("delete removed the wrong keys")
	}

	if err := s.Delete(ctx, "not/there"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestRedisStorageLock(t *testing.T) {
	ctx := context.Background()
	s1, mr := newTestStorage(t)
	s2 := newRedisStorageClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}), "")
	t.Cleanup(func() { _ = s2.Close() })

	if err := s1.Lock(ctx, "issue_cert_example.com"); err != nil {
		t.Fatal(err)
	}

	tctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	if err := s2.Lock(tctx, "issue_cert_example.com"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock = %v, want timeout", err)
	}

	// another instance must not release a lock it does not hold
	if err := s2.Unlock(ctx, "issue_cert_example.com"); err == nil {
		t.Fatal("unlock without holding succeeded")
	}

	done := make(chan error, 1)
	go func() { done <- s2.Lock(ctx, "issue_cert_example.com") }()

	time.Sleep(100 * time.Millisecond)

	if err := s1.Unlock(ctx, "issue_cert_example.com"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock was not acquired after unlock")
	}

	// a crashed holder's lock expires
	mr.FastForward(2 * redisLockTTL)

	if err := s1.Lock(ctx, "issue_cert_example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestOnDemandDecision(t *testing.T) {
	ask := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Query().Get("domain"), ".customer.net") {
			w.WriteHeader(http.StatusOK)

			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ask.Close()

	ctx := context.Background()

	decide, err := (&OnDemand{Ask: ask.URL, Allow: []string{"*.example.com"}}).decision([]string{"static.org"})
	if err != nil {
		t.Fatal(err)
	}

	for name, ok := range map[string]bool{
		"a.example.com":   true,
		"static.org":      true,
		"x.customer.net":  true,
		"evil.com":        false,
		"a.b.example.com": false,
	} {
		if got := decide(ctx, name) == nil; got != ok {
			t.Fatalf("%s allowed = %v, want %v", name, got, ok)
		}
	}

	decide, err = (&OnDemand{Allow: []string{"*.example.com"}}).decision(nil)
	if err != nil {
		t.Fatal(err)
	}
	if decide(ctx, "evil.com") == nil {
		t.Fatal("not allowed host passed without ask")
	}

	if _, err := (&OnDemand{}).decision(nil); err == nil {
		t.Fatal("on_demand without ask and allow accepted")
	}
	if _, err := (&OnDemand{Ask: "ftp://x"}).decision(nil); err == nil {
		t.Fatal("non http ask accepted")
	}
}

func TestNewValidation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	bad := []ACME{
		{Enabled: true, CacheDir: dir},
		{Enabled: true, CacheDir: dir, Domains: []string{"*.example.com"}},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.*.example.com"}, DNS: &DNS{Cloudflare: &DNSCloudflare{APIToken: "x"}}},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, CA: "nope"},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, KeyType: "dsa"},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, DisableHTTPChallenge: true, DisableTLSALPNChallenge: true},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, DNS: &DNS{}},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, DNS: &DNS{Cloudflare: &DNSCloudflare{APIToken: "x"}, Hetzner: &DNSHetzner{APIToken: "y"}}},
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com"}, EAB: &EAB{KeyID: "k"}},
	}

	for i := range bad {
		if m, err := New(ctx, &bad[i]); err == nil {
			m.Stop()
			t.Fatalf("config %d accepted: %+v", i, bad[i])
		}
	}

	good := []ACME{
		{Enabled: true, CacheDir: dir, Domains: []string{"a.com", "A.com."}},
		{Enabled: true, CacheDir: dir, Domains: []string{"*.example.com", "example.com"}, CA: "letsencrypt_staging", DNS: &DNS{Cloudflare: &DNSCloudflare{APIToken: "x"}}},
		{Enabled: true, CacheDir: dir, CA: "zerossl", KeyType: "rsa2048", EAB: &EAB{KeyID: "k", MACKey: "m"}, OnDemand: &OnDemand{Allow: []string{"*.a.com"}}},
	}

	for i := range good {
		m, err := New(ctx, &good[i])
		if err != nil {
			t.Fatalf("config %d: %v", i, err)
		}

		if i == 0 && (len(m.domains) != 1 || !m.TLSALPN()) {
			t.Fatalf("domains = %v, tls-alpn = %v", m.domains, m.TLSALPN())
		}
		if i == 1 && m.TLSALPN() {
			t.Fatal("tls-alpn must be off with dns challenge")
		}

		m.Stop()
	}

	if (&ACME{Domains: []string{"a"}}).Fingerprint() == (&ACME{Domains: []string{"b"}}).Fingerprint() {
		t.Fatal("fingerprint does not change")
	}
}

func TestHTTPChallengePassThrough(t *testing.T) {
	m, err := New(context.Background(), &ACME{Enabled: true, CacheDir: t.TempDir(), Domains: []string{"a.com"}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	rec := httptest.NewRecorder()
	if m.HandleHTTPChallenge(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil)) {
		t.Fatal("normal request handled as challenge")
	}

	// an unknown token is not answered
	rec = httptest.NewRecorder()
	if m.HandleHTTPChallenge(rec, httptest.NewRequest(http.MethodGet, "http://a.com/.well-known/acme-challenge/unknown", nil)) {
		t.Fatal("unknown challenge answered")
	}

	var nilManager *Manager
	if nilManager.HandleHTTPChallenge(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/x", nil)) {
		t.Fatal("nil manager handled request")
	}
}
