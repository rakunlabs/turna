package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

func echo(name string) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Backend", name)
			w.Header().Set("X-Path", r.URL.Path)
			w.Header().Set("X-Host", r.Host)
			w.Header().Set("X-Foo", r.Header.Get("Foo"))
			w.WriteHeader(http.StatusOK)
		})
	}}
}

func serve(t *testing.T, g *Gateway, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	mw, err := g.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mw(nil).ServeHTTP(rec, req)

	return rec
}

func str(s string) *string { return &s }

func TestGatewayPrecedence(t *testing.T) {
	for _, n := range []string{"gw-a", "gw-b", "gw-c", "gw-d", "gw-e", "gw-f"} {
		registry.GlobalReg.AddHttpMiddleware(n, echo(n))
	}

	g := &Gateway{Rules: []Rule{
		{Middlewares: []string{"gw-a"}},
		{Matches: []Match{{Path: &PathMatch{Type: PathPrefix, Value: "/api"}}}, Middlewares: []string{"gw-b"}},
		{Matches: []Match{{Path: &PathMatch{Type: PathPrefix, Value: "/api/v2/"}}}, Middlewares: []string{"gw-c"}},
		{Matches: []Match{{Path: &PathMatch{Type: PathExact, Value: "/api/v2/exact"}}}, Middlewares: []string{"gw-d"}},
		{Matches: []Match{{Path: &PathMatch{Value: "/api"}, Method: "post"}}, Middlewares: []string{"gw-e"}},
		{Matches: []Match{{
			Path:    &PathMatch{Value: "/api"},
			Headers: []ValueMatch{{Name: "x-version", Value: "v[0-9]+", Type: ValueRegex}},
			Query:   []ValueMatch{{Name: "debug", Value: "1"}},
		}}, Middlewares: []string{"gw-f"}},
	}}

	cases := []struct {
		method, target string
		header         map[string]string
		want           string
	}{
		{"GET", "/", nil, "gw-a"},
		{"GET", "/apix", nil, "gw-a"},
		{"GET", "/api", nil, "gw-b"},
		{"GET", "/api/v1", nil, "gw-b"},
		{"GET", "/api/v2", nil, "gw-c"},
		{"GET", "/api/v2/other", nil, "gw-c"},
		{"GET", "/api/v2/exact", nil, "gw-d"},
		{"POST", "/api/x", nil, "gw-e"},
		{"GET", "/api/x?debug=1", map[string]string{"X-Version": "v3"}, "gw-f"},
		{"GET", "/api/x?debug=1", map[string]string{"X-Version": "latest"}, "gw-b"},
	}

	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.target, nil)
		for k, v := range c.header {
			req.Header.Set(k, v)
		}

		rec := serve(t, g, req)
		if got := rec.Header().Get("X-Backend"); got != c.want {
			t.Errorf("%s %s %v: got %q want %q", c.method, c.target, c.header, got, c.want)
		}
	}
}

func TestGatewayFilters(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-filter", echo("gw-filter"))

	g := &Gateway{ListenerPort: 8080, Rules: []Rule{
		{
			Matches: []Match{{Path: &PathMatch{Value: "/old"}}},
			Filters: []Filter{
				{RequestHeaders: &HeaderModifier{Set: map[string]string{"Foo": "bar"}}},
				{ResponseHeaders: &HeaderModifier{Add: map[string]string{"X-Added": "1"}}},
				{Rewrite: &Rewrite{Hostname: "internal", Path: &PathModifier{ReplacePrefixMatch: str("/new")}}},
			},
			Middlewares: []string{"gw-filter"},
		},
		{
			Matches:     []Match{{Path: &PathMatch{Value: "/strip"}}},
			Filters:     []Filter{{Rewrite: &Rewrite{Path: &PathModifier{ReplacePrefixMatch: str("/")}}}},
			Middlewares: []string{"gw-filter"},
		},
		{
			Matches: []Match{{Path: &PathMatch{Value: "/secure"}}},
			Filters: []Filter{{Redirect: &Redirect{Scheme: "https", StatusCode: 301}}},
		},
		{
			Matches: []Match{{Path: &PathMatch{Value: "/moved"}}},
			Filters: []Filter{{Redirect: &Redirect{Hostname: "other.example.com", Path: &PathModifier{ReplaceFullPath: str("/here")}}}},
		},
	}}

	rec := serve(t, g, httptest.NewRequest("GET", "http://example.com/old/item", nil))
	if rec.Header().Get("X-Path") != "/new/item" || rec.Header().Get("X-Host") != "internal" ||
		rec.Header().Get("X-Foo") != "bar" || rec.Header().Get("X-Added") != "1" {
		t.Fatalf("rewrite headers = %v", rec.Header())
	}

	for target, want := range map[string]string{"/strip": "/", "/strip/a/b": "/a/b"} {
		rec = serve(t, g, httptest.NewRequest("GET", target, nil))
		if got := rec.Header().Get("X-Path"); got != want {
			t.Errorf("%s rewritten to %q, want %q", target, got, want)
		}
	}

	rec = serve(t, g, httptest.NewRequest("GET", "http://example.com:8080/secure/x?a=1", nil))
	if rec.Code != 301 || rec.Header().Get("Location") != "https://example.com/secure/x?a=1" {
		t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = serve(t, g, httptest.NewRequest("GET", "http://example.com:8080/moved/x", nil))
	if rec.Code != 302 || rec.Header().Get("Location") != "http://other.example.com:8080/here" {
		t.Fatalf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestGatewayNoMatchAndNoBackend(t *testing.T) {
	g := &Gateway{Rules: []Rule{
		{Matches: []Match{{Path: &PathMatch{Type: PathExact, Value: "/only"}}}},
	}}

	if rec := serve(t, g, httptest.NewRequest("GET", "/other", nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("no match = %d", rec.Code)
	}

	if rec := serve(t, g, httptest.NewRequest("GET", "/only", nil)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("no backend = %d", rec.Code)
	}
}

func TestGatewayInvalid(t *testing.T) {
	for _, g := range []*Gateway{
		{Rules: []Rule{{Matches: []Match{{Path: &PathMatch{Type: "glob", Value: "/"}}}}}},
		{Rules: []Rule{{Matches: []Match{{Path: &PathMatch{Type: PathRegex, Value: "("}}}}}},
		{Rules: []Rule{{Middlewares: []string{"gw-missing"}}}},
		{Rules: []Rule{{Filters: []Filter{{Redirect: &Redirect{StatusCode: 200}}}}}},
	} {
		if _, err := g.Middleware(); err == nil {
			t.Errorf("expected error for %+v", g.Rules[0])
		}
	}
}

func TestGatewayGRPCErrors(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("gw-bad-upstream", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message":"bad gateway"}`))
		})
	}})

	g := &Gateway{Rules: []Rule{
		{Matches: []Match{{Path: &PathMatch{Type: PathExact, Value: "/svc/Known"}}}, Middlewares: []string{"gw-bad-upstream"}},
	}}

	for path, want := range map[string]string{"/svc/Unknown": "12", "/svc/Known": "14"} {
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("Content-Type", "application/grpc")

		rec := serve(t, g, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Grpc-Status") != want || rec.Body.Len() != 0 {
			t.Errorf("%s: code=%d grpc-status=%q body=%q", path, rec.Code, rec.Header().Get("Grpc-Status"), rec.Body.String())
		}
	}
}
