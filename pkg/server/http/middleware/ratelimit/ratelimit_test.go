package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
)

func newHandler(t *testing.T, m *RateLimit) http.Handler {
	t.Helper()

	mw, err := m.Middleware(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func do(h http.Handler, remote string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestRateLimitAll(t *testing.T) {
	h := newHandler(t, &RateLimit{Requests: 2, Duration: time.Hour})

	for i, want := range []int{204, 204, 429} {
		if got := do(h, "192.0.2."+strconv.Itoa(i)+":1", nil).Code; got != want {
			t.Fatalf("request %d: status = %d, want %d", i, got, want)
		}
	}
}

func TestRateLimitIP(t *testing.T) {
	h := newHandler(t, &RateLimit{LimitType: "ip", Requests: 1, Duration: time.Hour})

	if do(h, "192.0.2.1:1", nil).Code != 204 || do(h, "192.0.2.1:2", nil).Code != 429 {
		t.Fatal("second request of the same ip should be limited")
	}

	if do(h, "192.0.2.2:1", nil).Code != 204 {
		t.Fatal("other ip should have its own limit")
	}
}

func TestRateLimitHeaderKey(t *testing.T) {
	h := newHandler(t, &RateLimit{Key: []string{"header:X-API-Key"}, Requests: 1, Duration: time.Hour})

	a := map[string]string{"X-API-Key": "a"}
	b := map[string]string{"X-API-Key": "b"}

	if do(h, "192.0.2.1:1", a).Code != 204 || do(h, "192.0.2.1:1", a).Code != 429 {
		t.Fatal("same key should be limited")
	}

	if do(h, "192.0.2.1:1", b).Code != 204 {
		t.Fatal("other key should have its own limit")
	}

	// without the header the client ip is used
	if do(h, "192.0.2.9:1", nil).Code != 204 || do(h, "192.0.2.9:1", nil).Code != 429 {
		t.Fatal("missing header should fall back to the ip")
	}
}

func TestRateLimitTrustedProxies(t *testing.T) {
	h := newHandler(t, &RateLimit{
		Key: []string{"ip"}, TrustedProxies: []string{"10.0.0.0/8"}, Requests: 1, Duration: time.Hour,
	})

	c1 := map[string]string{"X-Forwarded-For": "203.0.113.1"}
	c2 := map[string]string{"X-Forwarded-For": "203.0.113.2"}

	if do(h, "10.0.0.1:1", c1).Code != 204 || do(h, "10.0.0.1:1", c2).Code != 204 {
		t.Fatal("clients behind the proxy should have separate limits")
	}

	if do(h, "10.0.0.1:1", c1).Code != 429 {
		t.Fatal("client behind the proxy should be limited")
	}
}

func TestRateLimitHeaders(t *testing.T) {
	h := newHandler(t, &RateLimit{Requests: 2, Duration: time.Minute, Headers: true})

	rec := do(h, "192.0.2.1:1", nil)
	if rec.Header().Get("X-RateLimit-Limit") != "2" || rec.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatalf("headers = %v", rec.Header())
	}

	do(h, "192.0.2.1:1", nil)

	rec = do(h, "192.0.2.1:1", nil)
	if rec.Code != 429 || rec.Header().Get("X-RateLimit-Remaining") != "0" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d headers = %v", rec.Code, rec.Header())
	}
}

func TestRateLimitConfig(t *testing.T) {
	for _, m := range []RateLimit{
		{Requests: 0, Duration: time.Minute},
		{Requests: 1, Duration: 0},
		{Requests: 1, Duration: time.Minute, LimitType: "user"},
		{Requests: 1, Duration: time.Minute, Key: []string{"header"}},
		{Requests: 1, Duration: time.Minute, Key: []string{"path:x"}},
		{Requests: 1, Duration: time.Minute, Key: []string{"unknown"}},
		{Requests: 1, Duration: time.Minute, TrustedProxies: []string{"bad"}},
	} {
		if _, err := m.Middleware(t.Context()); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}

func TestSlidingWindow(t *testing.T) {
	s := newMemoryStore(10, time.Minute)
	at := func(window int64, elapsed time.Duration) time.Time {
		return time.Unix(0, window*int64(time.Minute)+int64(elapsed))
	}

	for i := range 10 {
		if !s.allowAt(at(100, time.Second), "k").allowed {
			t.Fatalf("hit %d rejected", i)
		}
	}

	if s.allowAt(at(100, 2*time.Second), "k").allowed {
		t.Fatal("hit over the limit allowed")
	}

	// half of the previous window is still inside the sliding window: 10*0.5
	// counted, so 5 more fit
	for i := range 5 {
		if !s.allowAt(at(101, 30*time.Second), "k").allowed {
			t.Fatalf("next window hit %d rejected", i)
		}
	}

	if res := s.allowAt(at(101, 30*time.Second), "k"); res.allowed || res.reset != 30*time.Second {
		t.Fatalf("result = %+v", res)
	}

	// two windows later everything expired
	if res := s.allowAt(at(103, 0), "k"); !res.allowed || res.remaining != 9 {
		t.Fatalf("result = %+v", res)
	}

	if len(s.entries) != 1 {
		t.Fatalf("entries = %d", len(s.entries))
	}
}

// Set RATELIMIT_TEST_REDIS=host:port to run against a real Redis.
func TestRateLimitRedis(t *testing.T) {
	addr := os.Getenv("RATELIMIT_TEST_REDIS")
	if addr == "" {
		t.Skip("RATELIMIT_TEST_REDIS is not set")
	}

	m := func() *RateLimit {
		return &RateLimit{
			LimitType: "ip", Requests: 2, Duration: time.Hour, Headers: true,
			Redis: &Redis{
				ConnConfig: storeredis.ConnConfig{Address: []string{addr}},
				KeyPrefix:  "turna_test_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_",
			},
		}
	}

	shared := m()
	a := newHandler(t, shared)
	b := newHandler(t, &RateLimit{
		LimitType: shared.LimitType, Requests: shared.Requests, Duration: shared.Duration, Redis: shared.Redis,
	})

	if do(a, "192.0.2.1:1", nil).Code != 204 || do(b, "192.0.2.1:1", nil).Code != 204 {
		t.Fatal("first two requests should pass")
	}

	if got := do(a, "192.0.2.1:1", nil); got.Code != 429 || got.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("instances should share the limit, status = %d", got.Code)
	}

	if do(b, "192.0.2.2:1", nil).Code != 204 {
		t.Fatal("other ip should pass")
	}
}

type failingStore struct{}

func (failingStore) allow(context.Context, string) (result, error) {
	return result{}, errors.New("down")
}

func TestRateLimitStoreFailure(t *testing.T) {
	for _, tt := range []struct {
		failClosed bool
		want       int
	}{{false, 204}, {true, 503}} {
		m := &RateLimit{Requests: 1, Duration: time.Minute, FailClosed: tt.failClosed}

		h := m.handler(failingStore{}, func(*http.Request) string { return "k" })(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
		)

		if got := do(h, "192.0.2.1:1", nil).Code; got != tt.want {
			t.Fatalf("fail_closed=%v status = %d, want %d", tt.failClosed, got, tt.want)
		}
	}
}
