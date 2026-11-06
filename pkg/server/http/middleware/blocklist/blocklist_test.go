package blocklist

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	mu    sync.Mutex
	hosts map[string][]string
	ptrs  map[string][]string
	calls int
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++

	if v, ok := f.hosts[host]; ok {
		return v, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func (f *fakeResolver) LookupAddr(_ context.Context, addr string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++

	if v, ok := f.ptrs[addr]; ok {
		return v, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: addr, IsNotFound: true}
}

func newHandler(t *testing.T, m *Blocklist) http.Handler {
	t.Helper()

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

type req struct {
	remote, path, ua string
}

func do(h http.Handler, r req) int {
	if r.path == "" {
		r.path = "/"
	}

	hr := httptest.NewRequest(http.MethodGet, r.path, nil)
	hr.RemoteAddr = r.remote
	if r.remote == "" {
		hr.RemoteAddr = "192.0.2.1:1234"
	}
	hr.Header.Set("User-Agent", r.ua)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, hr)

	return rec.Code
}

func TestBlocklistIP(t *testing.T) {
	h := newHandler(t, &Blocklist{Deny: Rules{IP: []string{"192.0.2.0/24", "2001:db8::1"}}})

	for _, c := range []struct {
		remote string
		want   int
	}{
		{"192.0.2.9:1", 403},
		{"[::ffff:192.0.2.9]:1", 403},
		{"[2001:db8::1]:1", 403},
		{"198.51.100.1:1", 204},
	} {
		if got := do(h, req{remote: c.remote}); got != c.want {
			t.Errorf("%s: status = %d, want %d", c.remote, got, c.want)
		}
	}
}

func TestBlocklistUserAgentAndURI(t *testing.T) {
	h := newHandler(t, &Blocklist{
		StatusCode: 404,
		Deny: Rules{
			UserAgent: []string{`(?i)sqlmap|nikto`},
			URI:       []string{`^/wp-admin`, `\.php$`},
		},
	})

	for _, c := range []struct {
		r    req
		want int
	}{
		{req{ua: "sqlmap/1.0"}, 404},
		{req{ua: "Nikto"}, 404},
		{req{path: "/wp-admin/x"}, 404},
		{req{path: "/index.php"}, 404},
		{req{path: "/api", ua: "curl"}, 204},
		{req{path: "/x/../wp-admin/"}, 404},
		{req{path: "//wp-admin"}, 404},
	} {
		if got := do(h, c.r); got != c.want {
			t.Errorf("%+v: status = %d, want %d", c.r, got, c.want)
		}
	}
}

func TestBlocklistAllowPrecedence(t *testing.T) {
	h := newHandler(t, &Blocklist{
		Allow: Rules{IP: []string{"10.0.0.0/8"}, UserAgent: []string{`^healthcheck$`}},
		Deny:  Rules{URI: []string{`^/admin`}},
	})

	if got := do(h, req{remote: "10.1.1.1:1", path: "/admin"}); got != 204 {
		t.Errorf("allowed ip status = %d, want 204", got)
	}

	if got := do(h, req{path: "/admin", ua: "healthcheck"}); got != 204 {
		t.Errorf("allowed user agent status = %d, want 204", got)
	}

	if got := do(h, req{path: "/admin"}); got != 403 {
		t.Errorf("status = %d, want 403", got)
	}
}

func TestBlocklistAllowURINormalized(t *testing.T) {
	h := newHandler(t, &Blocklist{
		Allow: Rules{URI: []string{`^/public/`}},
		Deny:  Rules{UserAgent: []string{`bot`}},
	})

	if got := do(h, req{path: "/public/a", ua: "bot"}); got != 204 {
		t.Errorf("allowed path status = %d, want 204", got)
	}

	if got := do(h, req{path: "/public/../admin", ua: "bot"}); got != 403 {
		t.Errorf("dot segments escaping an allowed path: status = %d, want 403", got)
	}
}

func TestBlocklistConfigUnchanged(t *testing.T) {
	m := &Blocklist{
		Deny:     Rules{IP: []string{"192.0.2.0/24"}, RDNS: []string{".example"}},
		DNSBL:    []string{"bl.example"},
		Allow:    Rules{URLs: []URL{{URL: filepath.Join(t.TempDir(), "allow.txt"), Type: " IP "}}},
		resolver: &fakeResolver{},
	}

	before, _ := json.Marshal(m)
	newHandler(t, m)

	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}
}

func TestBlocklistRDNS(t *testing.T) {
	res := &fakeResolver{
		ptrs: map[string][]string{
			"192.0.2.1": {"scanner.shodan.io."},
			"192.0.2.2": {"fake.shodan.io."},
			"192.0.2.3": {"host.example.com."},
		},
		hosts: map[string][]string{
			"scanner.shodan.io": {"192.0.2.1"},
			"fake.shodan.io":    {"203.0.113.1"},
		},
	}

	m := &Blocklist{Deny: Rules{RDNS: []string{".shodan.io"}}, resolver: res}
	h := newHandler(t, m)

	for _, c := range []struct {
		remote string
		want   int
	}{
		{"192.0.2.1:1", 403},
		// PTR does not resolve back, spoofed
		{"192.0.2.2:1", 204},
		{"192.0.2.3:1", 204},
		{"192.0.2.4:1", 204},
	} {
		if got := do(h, req{remote: c.remote}); got != c.want {
			t.Errorf("%s: status = %d, want %d", c.remote, got, c.want)
		}
	}

	calls := res.calls
	do(h, req{remote: "192.0.2.1:1"})
	if res.calls != calls {
		t.Error("cached lookup should not hit the resolver")
	}

	off := false
	h = newHandler(t, &Blocklist{Deny: Rules{RDNS: []string{"shodan.io"}}, RDNSVerify: &off, resolver: res})
	if got := do(h, req{remote: "192.0.2.2:1"}); got != 403 {
		t.Errorf("rdns_verify false: status = %d, want 403", got)
	}
}

func TestBlocklistNoDNSWithoutRules(t *testing.T) {
	res := &fakeResolver{}
	h := newHandler(t, &Blocklist{Deny: Rules{URI: []string{"^/x"}}, resolver: res})
	do(h, req{})

	if res.calls != 0 {
		t.Fatalf("resolver called %d times", res.calls)
	}
}

func TestBlocklistDNSBL(t *testing.T) {
	res := &fakeResolver{hosts: map[string][]string{
		"1.2.0.192.zen.example": {"127.0.0.2"},
		"1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.bl.example": {"127.0.0.4"},
		// public resolver refused by spamhaus, and a resolver rewriting NXDOMAIN
		"3.2.0.192.zen.example": {"127.255.255.254"},
		"4.2.0.192.zen.example": {"198.51.100.7"},
	}}

	h := newHandler(t, &Blocklist{DNSBL: []string{"bl.example", "zen.example."}, resolver: res})

	for _, c := range []struct {
		remote string
		want   int
	}{
		{"192.0.2.1:1", 403},
		{"192.0.2.2:1", 204},
		{"192.0.2.3:1", 204},
		{"192.0.2.4:1", 204},
		{"[2001:db8::1]:1", 403},
		{"[2001:db8::2]:1", 204},
	} {
		if got := do(h, req{remote: c.remote}); got != c.want {
			t.Errorf("%s: status = %d, want %d", c.remote, got, c.want)
		}
	}

	// negative results are cached too
	calls := res.calls
	do(h, req{remote: "192.0.2.2:1"})
	if res.calls != calls {
		t.Error("cached negative lookup should not hit the resolver")
	}
}

func TestReverseName(t *testing.T) {
	if got := reverseName(netip.MustParseAddr("192.0.2.1")); got != "1.2.0.192" {
		t.Errorf("ipv4 = %q", got)
	}

	want := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2"
	if got := reverseName(netip.MustParseAddr("2001:db8::1")); got != want {
		t.Errorf("ipv6 = %q", got)
	}
}

func TestBlocklistURLs(t *testing.T) {
	var (
		mu   sync.Mutex
		body = "# comment\n\n198.51.100.0/24 ; SBL\n203.0.113.5\nnot-an-ip\n"
		fail atomic.Bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	uaFile := filepath.Join(dir, "ua.txt")
	if err := os.WriteFile(uaFile, []byte("# bots\n(?i)badbot\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &Blocklist{
		Refresh: 20 * time.Millisecond,
		Deny: Rules{URLs: []URL{
			{URL: srv.URL, Type: "ip"},
			{URL: "file://" + uaFile, Type: "user_agent"},
		}},
		Allow: Rules{URLs: []URL{{URL: uaFile + ".missing", Type: "ip"}}},
	}
	h := newHandler(t, m)

	if got := do(h, req{remote: "198.51.100.7:1"}); got != 403 {
		t.Errorf("listed cidr status = %d, want 403", got)
	}
	if got := do(h, req{remote: "203.0.113.5:1"}); got != 403 {
		t.Errorf("listed ip status = %d, want 403", got)
	}
	if got := do(h, req{ua: "BadBot/2"}); got != 403 {
		t.Errorf("listed user agent status = %d, want 403", got)
	}
	if got := do(h, req{}); got != 204 {
		t.Errorf("status = %d, want 204", got)
	}

	// refresh replaces the list
	mu.Lock()
	body = "192.0.2.0/24\n"
	mu.Unlock()

	waitFor(t, func() bool { return do(h, req{}) == 403 })

	if got := do(h, req{remote: "198.51.100.7:1"}); got != 204 {
		t.Errorf("removed cidr status = %d, want 204", got)
	}

	// failures keep the previous list
	fail.Store(true)
	time.Sleep(60 * time.Millisecond)

	if got := do(h, req{}); got != 403 {
		t.Errorf("after failed refresh status = %d, want 403", got)
	}
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestBlocklistValidation(t *testing.T) {
	for name, m := range map[string]*Blocklist{
		"empty":     {},
		"ip":        {Deny: Rules{IP: []string{"nope"}}},
		"regex":     {Deny: Rules{URI: []string{"("}}},
		"url type":  {Deny: Rules{URLs: []URL{{URL: "/tmp/x", Type: "foo"}}}},
		"url empty": {Deny: Rules{URLs: []URL{{Type: "ip"}}}},
		"zone":      {DNSBL: []string{"."}},
		"proxies":   {Deny: Rules{IP: []string{"1.1.1.1"}}, TrustedProxies: []string{"x"}},
	} {
		if _, err := m.Middleware(t.Context()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLookupsBounded(t *testing.T) {
	l := newLookups(t.Context(), &fakeResolver{}, lookupsConfig{ttl: time.Hour, timeout: time.Second, size: 10})

	for i := range 100 {
		l.set(string(rune('a'+i)), cacheEntry{}, time.Hour)
	}

	if n := len(l.cache); n > 10 {
		t.Fatalf("cache size = %d, want <= 10", n)
	}
}
