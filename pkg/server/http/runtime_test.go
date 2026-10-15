package http

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/hello"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

type testRuntime struct {
	*Runtime

	t          *testing.T
	entrypoint string
	listener   net.Listener
}

// apply mirrors server.Manager.Apply for a single HTTP entrypoint.
func (rt *testRuntime) apply(h *HTTP) error {
	registry.Begin()

	p, err := rt.Prepare(h, []string{rt.entrypoint})
	if err != nil {
		registry.Rollback()

		return err
	}

	registry.Commit()
	p.Activate(func(string) (net.Listener, error) { return rt.listener, nil })

	return nil
}

func newTestRuntime(t *testing.T, entrypoint string) (*testRuntime, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	prevDrain := registry.DrainTimeout
	registry.DrainTimeout = 0

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	rt := &testRuntime{Runtime: NewRuntime(ctx, wg), t: t, entrypoint: entrypoint, listener: listener}

	t.Cleanup(func() {
		rt.Shutdown()
		cancel()
		_ = listener.Close()
		wg.Wait()
		registry.DrainTimeout = prevDrain
	})

	return rt, "http://" + listener.Addr().String()
}

func helloConfig(entrypoint, host, path, message string) *HTTP {
	return &HTTP{
		Routers: map[string]Router{
			"r": {Host: host, Path: []string{path}, Middlewares: []string{entrypoint + "-hello"}, EntryPoints: []string{entrypoint}},
		},
		Middlewares: map[string]HTTPMiddleware{
			entrypoint + "-hello": {HelloMiddleware: &hello.Hello{Message: message}},
		},
	}
}

func get(t *testing.T, url, host string) (int, string) {
	t.Helper()

	req, err := stdhttp.NewRequest(stdhttp.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}

	res, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)

	return res.StatusCode, string(body)
}

func TestRuntimeApplyReplacesRoutes(t *testing.T) {
	rt, url := newTestRuntime(t, "rt-replace")

	if err := rt.apply(helloConfig("rt-replace", "", "/v1/*", "first")); err != nil {
		t.Fatal(err)
	}

	if code, body := get(t, url+"/v1/x", ""); code != stdhttp.StatusOK || body != "first" {
		t.Fatalf("got %d %q", code, body)
	}

	if err := rt.apply(helloConfig("rt-replace", "", "/v2/*", "second")); err != nil {
		t.Fatal(err)
	}

	if code, _ := get(t, url+"/v1/x", ""); code != stdhttp.StatusNotFound {
		t.Fatalf("old route still served: %d", code)
	}
	if code, body := get(t, url+"/v2/x", ""); code != stdhttp.StatusOK || body != "second" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestRuntimeApplyFailureKeepsPrevious(t *testing.T) {
	rt, url := newTestRuntime(t, "rt-fail")

	if err := rt.apply(helloConfig("rt-fail", "", "/*", "ok")); err != nil {
		t.Fatal(err)
	}

	bad := helloConfig("rt-fail", "", "/*", "changed")
	bad.Routers["r"] = Router{Path: []string{"/*"}, Middlewares: []string{"rt-fail-hello", "missing"}, EntryPoints: []string{"rt-fail"}}

	if err := rt.apply(bad); err == nil {
		t.Fatal("expected error for missing middleware")
	}

	if code, body := get(t, url+"/", ""); code != stdhttp.StatusOK || body != "ok" {
		t.Fatalf("previous config not kept: %d %q", code, body)
	}

	m, err := registry.GlobalReg.GetHttpMiddleware("rt-fail-hello")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	m[0](nil).ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("registry middleware not restored: %q", rec.Body.String())
	}

	// a later valid apply still sees the kept middleware as unchanged
	if err := rt.apply(helloConfig("rt-fail", "", "/*", "ok")); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeKeepsUnchangedMiddleware(t *testing.T) {
	rt, _ := newTestRuntime(t, "rt-keep")

	if err := rt.apply(helloConfig("rt-keep", "", "/*", "same")); err != nil {
		t.Fatal(err)
	}
	first, _ := registry.GlobalReg.GetHttpMiddleware("rt-keep-hello")

	if err := rt.apply(helloConfig("rt-keep", "", "/other/*", "same")); err != nil {
		t.Fatal(err)
	}
	second, _ := registry.GlobalReg.GetHttpMiddleware("rt-keep-hello")
	if &first[0] != &second[0] {
		t.Fatal("unchanged middleware was rebuilt")
	}

	if err := rt.apply(helloConfig("rt-keep", "", "/*", "different")); err != nil {
		t.Fatal(err)
	}
	third, _ := registry.GlobalReg.GetHttpMiddleware("rt-keep-hello")
	if &first[0] == &third[0] {
		t.Fatal("changed middleware was not rebuilt")
	}
}

func TestRuntimeTLSReload(t *testing.T) {
	rt, url := newTestRuntime(t, "rt-tls")
	url = strings.Replace(url, "http://", "https://", 1)

	apply := func(marker string) {
		cfg := helloConfig("rt-tls", "", "/*", "secure")
		r := cfg.Routers["r"]
		r.TLS = &struct{}{}
		cfg.Routers["r"] = r
		cfg.TLS.Store = map[string][]Certificate{"default": {writeCertFiles(t, marker)}}

		if err := rt.apply(cfg); err != nil {
			t.Fatal(err)
		}
	}

	request := func() (*stdhttp.Response, string) {
		client := &stdhttp.Client{Transport: &stdhttp.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test
			ForceAttemptHTTP2: true,
		}}
		res, err := client.Get(url + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, _ := io.ReadAll(res.Body)

		return res, string(body)
	}

	apply("first.example.com")
	res, body := request()
	if body != "secure" || res.ProtoMajor != 2 {
		t.Fatalf("got %q proto %s", body, res.Proto)
	}
	if got := res.TLS.PeerCertificates[0].DNSNames; !slices.Contains(got, "first.example.com") {
		t.Fatalf("unexpected cert %v", got)
	}

	apply("second.example.com")
	res, _ = request()
	if got := res.TLS.PeerCertificates[0].DNSNames; !slices.Contains(got, "second.example.com") {
		t.Fatalf("certificate not reloaded: %v", got)
	}
}

func TestRuntimeWildcardHost(t *testing.T) {
	rt, url := newTestRuntime(t, "rt-wildcard")

	if err := rt.apply(helloConfig("rt-wildcard", "*.example.com", "/*", "wild")); err != nil {
		t.Fatal(err)
	}

	if code, body := get(t, url+"/", "api.example.com"); code != stdhttp.StatusOK || body != "wild" {
		t.Fatalf("got %d %q", code, body)
	}
	if code, body := get(t, url+"/", "a.b.example.com"); code != stdhttp.StatusOK || body != "wild" {
		t.Fatalf("nested subdomain: %d %q", code, body)
	}
	if code, _ := get(t, url+"/", "example.com"); code != stdhttp.StatusNotFound {
		t.Fatalf("apex matched wildcard: %d", code)
	}
	if code, _ := get(t, url+"/", "api.example.org"); code != stdhttp.StatusNotFound {
		t.Fatalf("other domain matched wildcard: %d", code)
	}
}

func TestRuleRouterLongestWildcard(t *testing.T) {
	router := NewRuleRouter()
	for _, host := range []string{"*.example.com", "*.b.example.com", "x.b.example.com", ""} {
		router.SetRule(RuleSelection{Host: host, Entrypoint: "web"})
		h := host
		router.GetMux(RuleSelection{Host: host, Entrypoint: "web"}).Handle("/*", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
			_, _ = w.Write([]byte(h))
		}))
	}

	for host, want := range map[string]string{
		"x.b.example.com":   "x.b.example.com",
		"y.b.example.com":   "*.b.example.com",
		"z.y.b.example.com": "*.b.example.com",
		"a.example.com":     "*.example.com",
		"other.org":         "",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
		req.Host = host
		router.Serve("web").ServeHTTP(rec, req)

		if rec.Body.String() != want {
			t.Errorf("%s routed to %q, want %q", host, rec.Body.String(), want)
		}
	}
}

// Connections accepted before client certificates were required are closed
// when the requirement is turned on; unchanged settings keep connections.
func TestRuntimeClosesConnectionsOnClientAuthChange(t *testing.T) {
	caFile, issue := testClientPKI(t)
	ca, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}

	rt, addr := newTestRuntime(t, "mtls")
	addr = strings.Replace(addr, "http://", "https://", 1)

	cfg := func(message string, entry *EntryPointTLS) *HTTP {
		h := helloConfig("mtls", "", "/*", message)
		r := h.Routers["r"]
		r.TLS = &struct{}{}
		h.Routers["r"] = r
		h.TLS.Store = map[string][]Certificate{"default": {writeCertFiles(t, "localhost")}}

		if entry != nil {
			h.TLS.EntryPoints = map[string]EntryPointTLS{"mtls": *entry}
		}

		return h
	}

	if err := rt.apply(cfg("open", nil)); err != nil {
		t.Fatal(err)
	}

	client := &stdhttp.Client{Transport: &stdhttp.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server
		ForceAttemptHTTP2: true,
	}}

	get := func() (string, error) {
		res, err := client.Get(addr + "/")
		if err != nil {
			return "", err
		}
		defer func() { _ = res.Body.Close() }()

		b, err := io.ReadAll(res.Body)

		return string(b), err
	}

	if body, err := get(); err != nil || !strings.Contains(body, "open") {
		t.Fatalf("before: %q %v", body, err)
	}

	// same client settings: the connection stays and sees the new routes
	if err := rt.apply(cfg("still open", nil)); err != nil {
		t.Fatal(err)
	}

	if body, err := get(); err != nil || !strings.Contains(body, "still open") {
		t.Fatalf("unchanged settings: %q %v", body, err)
	}

	if err := rt.apply(cfg("secret", &EntryPointTLS{ClientCAs: []string{string(ca)}})); err != nil {
		t.Fatal(err)
	}

	if body, err := get(); err == nil {
		t.Fatalf("connection without a client certificate still served: %q", body)
	}

	now := time.Now()
	c := issue(now.Add(-time.Hour), now.Add(time.Hour))
	client.Transport.(*stdhttp.Transport).TLSClientConfig.Certificates = []tls.Certificate{c}

	if body, err := get(); err != nil || !strings.Contains(body, "secret") {
		t.Fatalf("with certificate: %q %v", body, err)
	}
}
