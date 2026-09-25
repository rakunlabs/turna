package service

import (
	"context"
	"io"
	stdlog "log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

func namedBackend(t *testing.T, name string, status *atomic.Int32) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := http.StatusOK
		if status != nil {
			code = int(status.Load())
		}

		w.WriteHeader(code)
		_, _ = io.WriteString(w, name)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func newServiceHandler(t *testing.T, ctx context.Context, m *Service) http.Handler {
	t.Helper()

	mws, err := m.Middleware(ctx)
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	var h http.Handler = http.NotFoundHandler()
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}

	// the router sets the turna context for every request
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, r = tcontext.New(w, r)
		h.ServeHTTP(w, r)
	})
}

func call(h http.Handler, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestWeightedRoundRobin(t *testing.T) {
	a := namedBackend(t, "a", nil)
	b := namedBackend(t, "b", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Servers: []Server{{URL: a.URL, Weight: 3}, {URL: b.URL, Weight: 1}},
	}})

	counts := map[string]int{}
	for range 8 {
		counts[call(h).Body.String()]++
	}

	if counts["a"] != 6 || counts["b"] != 2 {
		t.Fatalf("counts = %v, want a:6 b:2", counts)
	}
}

func TestWeightedRandom(t *testing.T) {
	a := namedBackend(t, "a", nil)
	b := namedBackend(t, "b", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Strategy: StrategyRandom,
		Servers:  []Server{{URL: a.URL, Weight: 9}, {URL: b.URL, Weight: 1}},
	}})

	counts := map[string]int{}
	for range 200 {
		counts[call(h).Body.String()]++
	}

	if counts["a"] < counts["b"]*3 {
		t.Fatalf("weights not applied: %v", counts)
	}
}

func TestLeastConn(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		_, _ = io.WriteString(w, "slow")
	}))
	defer slow.Close()

	fast := namedBackend(t, "fast", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Strategy: StrategyLeastConn,
		Servers:  []Server{{URL: slow.URL}, {URL: fast.URL}},
	}})

	// keep calling until the slow backend holds one active request
	var wg sync.WaitGroup
	for {
		rec := make(chan string, 1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			rec <- call(h).Body.String()
		}()

		select {
		case <-started:
		case <-rec:
			continue
		}

		break
	}

	for range 5 {
		if got := call(h).Body.String(); got != "fast" {
			t.Fatalf("got %q, want fast while slow is busy", got)
		}
	}

	close(release)
	wg.Wait()
}

func TestStickyCookie(t *testing.T) {
	a := namedBackend(t, "a", nil)
	b := namedBackend(t, "b", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Servers: []Server{{URL: a.URL}, {URL: b.URL}},
		Sticky:  &Sticky{Cookie: &StickyCookie{Name: "lb"}},
	}})

	rec := call(h)
	first := rec.Body.String()

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "lb" || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookies %v", cookies)
	}

	for range 5 {
		rec := call(h, cookies[0])
		if got := rec.Body.String(); got != first {
			t.Fatalf("sticky target changed: %q -> %q", first, got)
		}

		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("cookie must not be set again for a sticky hit")
		}
	}

	// unknown value picks a new target and resets the cookie
	rec = call(h, &http.Cookie{Name: "lb", Value: "unknown"})
	if len(rec.Result().Cookies()) != 1 {
		t.Fatal("cookie must be reset for an unknown target")
	}
}

func TestRetryOnUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	ok := namedBackend(t, "ok", nil)

	h := newServiceHandler(t, context.Background(), &Service{
		Retry: 1,
		LoadBalancer: LoadBalancer{
			Servers: []Server{{URL: deadURL}, {URL: ok.URL}},
		},
	})

	for range 4 {
		if rec := call(h); rec.Body.String() != "ok" {
			t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
		}
	}
}

func TestPassiveHealthCheck(t *testing.T) {
	status := &atomic.Int32{}
	status.Store(http.StatusBadGateway)

	bad := namedBackend(t, "bad", status)
	good := namedBackend(t, "good", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Servers: []Server{{URL: bad.URL}, {URL: good.URL}},
		PassiveHealthCheck: &PassiveHealthCheck{
			MaxFails:     2,
			FailTimeout:  time.Minute,
			FailStatuses: []int{http.StatusBadGateway},
		},
	}})

	for range 4 {
		call(h)
	}

	for range 5 {
		if got := call(h).Body.String(); got != "good" {
			t.Fatalf("got %q, bad upstream must be ejected", got)
		}
	}
}

func TestActiveHealthCheck(t *testing.T) {
	healthy := &atomic.Bool{}
	healthy.Store(true)

	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if !healthy.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			return
		}

		_, _ = io.WriteString(w, "flaky")
	}))
	defer flaky.Close()

	good := namedBackend(t, "good", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := newServiceHandler(t, ctx, &Service{LoadBalancer: LoadBalancer{
		Servers: []Server{{URL: flaky.URL}, {URL: good.URL}},
		HealthCheck: &HealthCheck{
			Path:     "/health",
			Interval: 20 * time.Millisecond,
		},
	}})

	healthy.Store(false)

	waitFor(t, func() bool {
		for range 4 {
			if call(h).Body.String() == "flaky" {
				return false
			}
		}

		return true
	})

	healthy.Store(true)

	waitFor(t, func() bool {
		for range 4 {
			if call(h).Body.String() == "flaky" {
				return true
			}
		}

		return false
	})
}

func TestAllUnhealthyFailOpen(t *testing.T) {
	a := namedBackend(t, "a", nil)

	lb, err := newLBBalancer([]*ProxyTarget{{URL: mustURL(t, a.URL), state: newTargetState(mustURL(t, a.URL), nil)}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	lb.targets[0].state.healthy.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, req = tcontext.New(nil, req)

	if tgt, err := lb.NextTarget(httptest.NewRecorder(), req); err != nil || tgt == nil {
		t.Fatalf("fail open expected, got %v %v", tgt, err)
	}
}

func TestPrefixBalancerWeighted(t *testing.T) {
	a := namedBackend(t, "a", nil)
	b := namedBackend(t, "b", nil)
	d := namedBackend(t, "default", nil)

	h := newServiceHandler(t, context.Background(), &Service{PrefixBalancer: PrefixBalancer{
		Prefixes: []PrefixServers{{
			Prefix:  "/api",
			Servers: []Server{{URL: a.URL, Weight: 2}, {URL: b.URL}},
		}},
		DefaultServers: []Server{{URL: d.URL}},
	}})

	counts := map[string]int{}
	for range 6 {
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		counts[rec.Body.String()]++
	}

	if counts["a"] != 4 || counts["b"] != 2 {
		t.Fatalf("counts = %v", counts)
	}

	if got := call(h).Body.String(); got != "default" {
		t.Fatalf("got %q, want default", got)
	}
}

func TestMirror(t *testing.T) {
	primary := namedBackend(t, "primary", nil)

	type mirrored struct {
		path, body, host string
	}
	got := make(chan mirrored, 1)

	shadow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- mirrored{r.URL.RequestURI(), string(body), r.Host}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer shadow.Close()

	h := newServiceHandler(t, context.Background(), &Service{
		LoadBalancer: LoadBalancer{Servers: []Server{{URL: primary.URL}}},
		Mirror:       &Mirror{Servers: []MirrorServer{{URL: shadow.URL}}},
	})

	req := httptest.NewRequest(http.MethodPost, "http://example.com/items?x=1", strings.NewReader("payload"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Body.String() != "primary" || rec.Code != http.StatusOK {
		t.Fatalf("client response changed: %d %q", rec.Code, rec.Body.String())
	}

	select {
	case m := <-got:
		if m.path != "/items?x=1" || m.body != "payload" || m.host != "example.com" {
			t.Fatalf("mirrored request = %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mirror request not received")
	}
}

func TestMirrorBodyTooLarge(t *testing.T) {
	primaryBody := make(chan string, 1)
	primary := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		primaryBody <- string(b)
	}))
	defer primary.Close()

	mirrored := &atomic.Bool{}
	shadow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mirrored.Store(true)
	}))
	defer shadow.Close()

	h := newServiceHandler(t, context.Background(), &Service{
		LoadBalancer: LoadBalancer{Servers: []Server{{URL: primary.URL}}},
		Mirror:       &Mirror{Servers: []MirrorServer{{URL: shadow.URL}}, MaxBodySize: 4},
	})

	body := strings.Repeat("x", 100)
	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(body)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got := <-primaryBody; got != body {
		t.Fatalf("primary body = %d bytes, want %d", len(got), len(body))
	}

	time.Sleep(50 * time.Millisecond)

	if mirrored.Load() {
		t.Fatal("large body must not be mirrored")
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*Service{
		{LoadBalancer: LoadBalancer{Strategy: "nope", Servers: []Server{{URL: "http://a"}}}},
		{LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://a", Weight: -1}}}},
		{LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://a"}}, HealthCheck: &HealthCheck{Status: "abc"}}},
		{LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://a"}}, Sticky: &Sticky{Cookie: &StickyCookie{SameSite: "x"}}}},
		{LoadBalancer: LoadBalancer{Servers: []Server{{URL: "http://a"}}}, Mirror: &Mirror{Servers: []MirrorServer{{URL: "http://m", Percent: ptr(150.0)}}}},
	} {
		if _, err := m.Middleware(context.Background()); err == nil {
			t.Fatalf("expected error for %+v", m)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("condition not met in time")
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

// Health checks and mirrors use the proxy transport, so insecure_skip_verify
// applies to them too.
func TestInsecureSkipVerifyHealthCheckAndMirror(t *testing.T) {
	for _, insecure := range []bool{true, false} {
		var healthHits, mirrorHits atomic.Int32

		backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				healthHits.Add(1)
			}

			_, _ = io.WriteString(w, "tls")
		}))
		backend.Config.ErrorLog = stdlog.New(io.Discard, "", 0)
		backend.StartTLS()
		defer backend.Close()

		shadow := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mirrorHits.Add(1)
		}))
		shadow.Config.ErrorLog = stdlog.New(io.Discard, "", 0)
		shadow.StartTLS()
		defer shadow.Close()

		ctx, cancel := context.WithCancel(context.Background())

		h := newServiceHandler(t, ctx, &Service{
			InsecureSkipVerify: insecure,
			LoadBalancer: LoadBalancer{
				Servers: []Server{{URL: backend.URL}},
				HealthCheck: &HealthCheck{
					Path:     "/health",
					Interval: 20 * time.Millisecond,
				},
			},
			Mirror: &Mirror{Servers: []MirrorServer{{URL: shadow.URL}}},
		})

		rec := call(h)

		if insecure {
			if rec.Body.String() != "tls" {
				t.Fatalf("insecure: proxy body %q code %d", rec.Body.String(), rec.Code)
			}

			waitFor(t, func() bool { return healthHits.Load() > 0 && mirrorHits.Load() > 0 })
		} else {
			time.Sleep(200 * time.Millisecond)

			if healthHits.Load() != 0 || mirrorHits.Load() != 0 {
				t.Fatalf("verify: unverified certificate accepted, health %d mirror %d", healthHits.Load(), mirrorHits.Load())
			}
		}

		cancel()
	}
}
