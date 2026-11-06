package badbehavior

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	storeredis "github.com/rakunlabs/cache/store/redis"
)

func newHandler(t *testing.T, m *BadBehavior) http.Handler {
	t.Helper()

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		_, _ = w.Write([]byte("ok"))
	}))
}

func do(h http.Handler, remote, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remote

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func config() *BadBehavior {
	return &BadBehavior{Threshold: 3, Window: time.Minute, BanTime: time.Hour}
}

func TestBadBehaviorBans(t *testing.T) {
	h := newHandler(t, config())

	for i := range 3 {
		if got := do(h, "192.0.2.1:1", "/missing").Code; got != http.StatusNotFound {
			t.Fatalf("request %d: status = %d, want 404", i, got)
		}
	}

	rec := do(h, "192.0.2.1:1", "/")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("banned client status = %d, want 403", rec.Code)
	}

	if ra := rec.Header().Get("Retry-After"); ra != "3600" {
		t.Fatalf("Retry-After = %q, want 3600", ra)
	}

	if got := do(h, "192.0.2.2:1", "/").Code; got != http.StatusOK {
		t.Fatalf("other client status = %d, want 200", got)
	}
}

func TestBadBehaviorGoodResponsesNotCounted(t *testing.T) {
	h := newHandler(t, config())

	for range 10 {
		do(h, "192.0.2.1:1", "/")
	}

	if got := do(h, "192.0.2.1:1", "/").Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
}

func TestBadBehaviorStatusCodes(t *testing.T) {
	m := config()
	m.StatusCodes = []string{"500-599"}
	m.StatusCode = http.StatusTooManyRequests
	h := newHandler(t, m)

	for range 5 {
		do(h, "192.0.2.1:1", "/missing")
	}

	if got := do(h, "192.0.2.1:1", "/").Code; got != http.StatusOK {
		t.Fatalf("404 should not be counted, status = %d", got)
	}
}

func TestBadBehaviorAllow(t *testing.T) {
	m := config()
	m.Allow = []string{"192.0.2.0/24"}
	h := newHandler(t, m)

	for range 5 {
		do(h, "192.0.2.1:1", "/missing")
	}

	if got := do(h, "192.0.2.1:1", "/").Code; got != http.StatusOK {
		t.Fatalf("allowed client status = %d, want 200", got)
	}
}

func TestBadBehaviorTrustedProxies(t *testing.T) {
	m := config()
	m.TrustedProxies = []string{"10.0.0.0/8"}
	h := newHandler(t, m)

	req := func(xff string) int {
		r := httptest.NewRequest(http.MethodGet, "/missing", nil)
		r.RemoteAddr = "10.0.0.1:1"
		r.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		return rec.Code
	}

	for range 3 {
		req("198.51.100.1")
	}

	if got := req("198.51.100.1"); got != http.StatusForbidden {
		t.Fatalf("banned client status = %d, want 403", got)
	}

	if got := req("198.51.100.2"); got != http.StatusNotFound {
		t.Fatalf("other client behind proxy status = %d, want 404", got)
	}
}

func TestBadBehaviorValidation(t *testing.T) {
	for name, m := range map[string]*BadBehavior{
		"threshold": {Threshold: -1},
		"window":    {Window: -time.Second},
		"ban_time":  {BanTime: -time.Second},
		"status":    {Threshold: 1, Window: time.Minute, BanTime: time.Hour, StatusCodes: []string{"abc"}},
		"allow":     {Threshold: 1, Window: time.Minute, BanTime: time.Hour, Allow: []string{"nope"}},
	} {
		if _, err := m.Middleware(t.Context()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestBadBehaviorDefaults(t *testing.T) {
	m := &BadBehavior{}
	before, _ := json.Marshal(m)

	h := newHandler(t, m)

	// the config is fingerprinted on reload, defaults must not be written back
	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}

	// default threshold 30
	for range 29 {
		do(h, "192.0.2.1:1", "/missing")
	}
	if rec := do(h, "192.0.2.1:1", "/"); rec.Code != http.StatusOK {
		t.Fatalf("banned before threshold: %d", rec.Code)
	}
	do(h, "192.0.2.1:1", "/missing")
	if rec := do(h, "192.0.2.1:1", "/"); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestMemoryStoreExpiry(t *testing.T) {
	s := newMemoryStore(t.Context(), 2, time.Minute, time.Hour)
	now := time.Now()

	s.hitAt(now, "a")
	// window passed, the count starts over
	if s.hitAt(now.Add(2*time.Minute), "a") {
		t.Fatal("hit after the window should not ban")
	}

	if !s.hitAt(now.Add(2*time.Minute+time.Second), "a") {
		t.Fatal("second hit in the window should ban")
	}

	if s.bannedAt(now.Add(time.Hour), "a") == 0 {
		t.Fatal("client should still be banned")
	}

	s.gc(now.Add(3 * time.Hour))

	if len(s.bans) != 0 || len(s.counter) != 0 {
		t.Fatalf("gc left entries: bans=%d counter=%d", len(s.bans), len(s.counter))
	}
}

func TestStatusRecorderUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &statusRecorder{ResponseWriter: rec}

	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Fatal(err)
	}

	if !rec.Flushed || w.code != http.StatusOK {
		t.Fatalf("flushed=%v code=%d", rec.Flushed, w.code)
	}
}

// Set BADBEHAVIOR_TEST_REDIS=host:port to run against a real Redis instead
// of miniredis.
func TestBadBehaviorRedis(t *testing.T) {
	addr := os.Getenv("BADBEHAVIOR_TEST_REDIS")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	}

	prefix := "turna_test_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_"
	m := func() *BadBehavior {
		c := config()
		c.Redis = &Redis{ConnConfig: storeredis.ConnConfig{Address: []string{addr}}, KeyPrefix: prefix}

		return c
	}

	// two instances share the counter
	h1, h2 := newHandler(t, m()), newHandler(t, m())

	do(h1, "192.0.2.1:1", "/missing")
	do(h2, "192.0.2.1:1", "/missing")
	do(h1, "192.0.2.1:1", "/missing")

	if got := do(h2, "192.0.2.1:1", "/").Code; got != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", got)
	}
}
