package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

func callNext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}

func addGateway(t *testing.T, name string, g *Gateway) {
	t.Helper()

	mw, err := g.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	registry.GlobalReg.AddHttpMiddleware(name, []func(http.Handler) http.Handler{mw})
}

func TestGatewayNestedPassthrough(t *testing.T) {
	addGateway(t, "gw-nested-inner", &Gateway{Passthrough: true, Rules: []Rule{{
		Filters: []Filter{{RequestHeaders: &HeaderModifier{Set: map[string]string{"Foo": "inner"}}}},
	}}})
	registry.GlobalReg.AddHttpMiddleware("gw-nested-pass", []func(http.Handler) http.Handler{callNext})
	registry.GlobalReg.AddHttpMiddleware("gw-nested-echo", echo("gw-nested-echo"))

	// the chain ends without a backend, it must answer with the rule status
	// instead of calling the next handler of the inner gateway again
	g := &Gateway{Rules: []Rule{{Middlewares: []string{"gw-nested-inner", "gw-nested-pass"}, Status: http.StatusTeapot}}}
	if rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil)); rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d", rec.Code)
	}

	g = &Gateway{Rules: []Rule{{Middlewares: []string{"gw-nested-inner", "gw-nested-echo"}}}}
	if rec := serve(t, g, httptest.NewRequest(http.MethodGet, "/", nil)); rec.Header().Get("X-Foo") != "inner" {
		t.Fatalf("headers = %v", rec.Header())
	}
}

func TestGatewayBackendPrefixRewrite(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-bp-echo", echo("gw-bp-echo"))
	addGateway(t, "gw-bp-filters", &Gateway{Passthrough: true, Rules: []Rule{{
		Filters: []Filter{{Rewrite: &Rewrite{Path: &PathModifier{ReplacePrefixMatch: str("/v2")}}}},
	}}})
	addGateway(t, "gw-bp-redirect", &Gateway{Passthrough: true, Rules: []Rule{{
		Filters: []Filter{{Redirect: &Redirect{Path: &PathModifier{ReplacePrefixMatch: str("/new")}}}},
	}}})

	g := &Gateway{Rules: []Rule{
		{
			Matches:  []Match{{Path: &PathMatch{Value: "/api"}}},
			Backends: []Backend{{Weight: 1, Middlewares: []string{"gw-bp-filters", "gw-bp-echo"}}},
		},
		{
			Matches:  []Match{{Path: &PathMatch{Value: "/old"}}},
			Backends: []Backend{{Weight: 1, Middlewares: []string{"gw-bp-redirect"}}},
		},
	}}

	if got := serve(t, g, httptest.NewRequest(http.MethodGet, "/api/users", nil)).Header().Get("X-Path"); got != "/v2/users" {
		t.Fatalf("rewrite = %q", got)
	}

	if got := serve(t, g, httptest.NewRequest(http.MethodGet, "http://example.com/old/x", nil)).Header().Get("Location"); got != "http://example.com/new/x" {
		t.Fatalf("redirect = %q", got)
	}
}

func TestGatewayDotSegments(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-dot-public", echo("public"))
	registry.GlobalReg.AddHttpMiddleware("gw-dot-admin", echo("admin"))

	g := &Gateway{Rules: []Rule{
		{Matches: []Match{{Path: &PathMatch{Value: "/public"}}}, Middlewares: []string{"gw-dot-public"}},
		{Matches: []Match{{Path: &PathMatch{Value: "/admin"}}}, Middlewares: []string{"gw-dot-admin"}, Status: http.StatusForbidden},
	}}

	cases := map[string]struct{ backend, path string }{
		"/public/../admin":     {"admin", "/admin"},
		"/public/%2e%2e/admin": {"admin", "/admin"},
		"/public/./a/../b":     {"public", "/public/b"},
		"/public/a/..":         {"public", "/public/"},
		"/../../public/x":      {"public", "/public/x"},
		"/public/..%2fadmin":   {"admin", "/admin"},
		"/public/file..name/x": {"public", "/public/file..name/x"},
	}

	for target, want := range cases {
		rec := serve(t, g, httptest.NewRequest(http.MethodGet, target, nil))
		if got := rec.Header().Get("X-Backend"); got != want.backend {
			t.Errorf("%s: backend %q want %q", target, got, want.backend)
		}

		if got := rec.Header().Get("X-Path"); got != want.path {
			t.Errorf("%s: path %q want %q", target, got, want.path)
		}
	}
}

func TestGatewayPrefixRewriteKeepsEscaping(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-esc", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Raw", r.URL.EscapedPath())
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Matches:     []Match{{Path: &PathMatch{Value: "/api"}}},
		Filters:     []Filter{{Rewrite: &Rewrite{Path: &PathModifier{ReplacePrefixMatch: str("/v2")}}}},
		Middlewares: []string{"gw-esc"},
	}}}

	if got := serve(t, g, httptest.NewRequest(http.MethodGet, "/api/a%2Fb", nil)).Header().Get("X-Raw"); got != "/v2/a%2Fb" {
		t.Fatalf("escaped path = %q", got)
	}
}

func TestGatewayRewriteAfterPathChange(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-shorten", []func(http.Handler) http.Handler{func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/x"
			next.ServeHTTP(w, r)
		})
	}})
	registry.GlobalReg.AddHttpMiddleware("gw-shorten-echo", echo("echo"))

	g := &Gateway{Rules: []Rule{{
		Matches: []Match{{Path: &PathMatch{Value: "/longer/prefix"}}},
		Filters: []Filter{
			{Middlewares: []string{"gw-shorten"}},
			{Rewrite: &Rewrite{Path: &PathModifier{ReplacePrefixMatch: str("/new")}}},
		},
		Middlewares: []string{"gw-shorten-echo"},
	}}}

	if got := serve(t, g, httptest.NewRequest(http.MethodGet, "/longer/prefix/a", nil)).Header().Get("X-Path"); got != "/x" {
		t.Fatalf("path = %q", got)
	}
}

func TestGatewayHostHeaderCase(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-host", echo("gw-host"))

	g := &Gateway{Rules: []Rule{{
		Filters:     []Filter{{RequestHeaders: &HeaderModifier{Set: map[string]string{"host": "internal"}}}},
		Middlewares: []string{"gw-host"},
	}}}

	if got := serve(t, g, httptest.NewRequest(http.MethodGet, "http://example.com/", nil)).Header().Get("X-Host"); got != "internal" {
		t.Fatalf("host = %q", got)
	}
}

func TestGatewayHostHeaderMatch(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-hm-yes", echo("yes"))
	registry.GlobalReg.AddHttpMiddleware("gw-hm-no", echo("no"))

	g := &Gateway{Rules: []Rule{
		{Matches: []Match{{Headers: []ValueMatch{{Name: "host", Value: "a.example.com"}}}}, Middlewares: []string{"gw-hm-yes"}},
		{Middlewares: []string{"gw-hm-no"}},
	}}

	for target, want := range map[string]string{
		"http://a.example.com/": "yes",
		"http://b.example.com/": "no",
	} {
		if got := serve(t, g, httptest.NewRequest(http.MethodGet, target, nil)).Header().Get("X-Backend"); got != want {
			t.Errorf("%s: got %q want %q", target, got, want)
		}
	}
}

func TestGatewayValueRegexAnchored(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-re-yes", echo("yes"))
	registry.GlobalReg.AddHttpMiddleware("gw-re-no", echo("no"))

	g := &Gateway{Rules: []Rule{
		{Matches: []Match{{
			Headers: []ValueMatch{{Name: "X-Version", Value: "v[0-9]+", Type: ValueRegex}},
			Query:   []ValueMatch{{Name: "q", Value: "a|b", Type: ValueRegex}},
		}}, Middlewares: []string{"gw-re-yes"}},
		{Middlewares: []string{"gw-re-no"}},
	}}

	for target, want := range map[string]map[string]string{
		"/?q=a":  {"v1": "yes", "xv1y": "no"},
		"/?q=ab": {"v1": "no"},
		"/?q=b":  {"v10": "yes"},
	} {
		for version, backend := range want {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("X-Version", version)

			if got := serve(t, g, req).Header().Get("X-Backend"); got != backend {
				t.Errorf("%s %s: got %q want %q", target, version, got, backend)
			}
		}
	}
}

func TestGatewayInformationalResponse(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-hints", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Link", "</a.css>; rel=preload")
			w.WriteHeader(http.StatusEarlyHints)
			w.WriteHeader(http.StatusOK)
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Timeout:     time.Second,
		Filters:     []Filter{{ResponseHeaders: &HeaderModifier{Set: map[string]string{"X-Final": "1"}}}},
		Middlewares: []string{"gw-hints"},
	}}}

	srv := httptest.NewServer(mustMiddleware(t, g))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Final") != "1" {
		t.Fatalf("status=%d headers=%v", resp.StatusCode, resp.Header)
	}
}

func mustMiddleware(t *testing.T, g *Gateway) http.Handler {
	t.Helper()

	mw, err := g.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(nil)
}

func TestIsGRPC(t *testing.T) {
	for ct, want := range map[string]bool{
		"application/grpc":               true,
		"application/grpc+proto":         true,
		"application/grpc; charset=utf8": true,
		"application/grpc-web":           false,
		"application/grpc-web+proto":     false,
		"application/json":               false,
	} {
		if got := isGRPCContentType(ct); got != want {
			t.Errorf("%q: got %v", ct, got)
		}
	}
}
