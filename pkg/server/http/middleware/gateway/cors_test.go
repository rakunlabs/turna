package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

func corsRequest(method, origin string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "/", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}

	for k, v := range headers {
		r.Header.Set(k, v)
	}

	return r
}

func TestCORS(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("cors-backend", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}})

	multi := &CORS{
		AllowOrigins:     []string{"https://www.foo.com", "https://*.bar.com"},
		AllowMethods:     []string{"GET", "OPTIONS"},
		AllowHeaders:     []string{"x-header-1", "x-header-2"},
		ExposeHeaders:    []string{"x-header-3", "x-header-4"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	wildcard := &CORS{AllowOrigins: []string{"*"}, AllowMethods: []string{"*"}, AllowHeaders: []string{"*"}, AllowCredentials: true}

	preflight := map[string]string{"Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "x-header-1, x-header-2"}

	for _, tt := range []struct {
		name    string
		cors    *CORS
		req     *http.Request
		status  int
		headers map[string]string
		absent  []string
	}{
		{
			name: "preflight exact origin", cors: multi,
			req:    corsRequest(http.MethodOptions, "https://www.foo.com", preflight),
			status: http.StatusNoContent,
			headers: map[string]string{
				"Access-Control-Allow-Origin":      "https://www.foo.com",
				"Access-Control-Allow-Methods":     "GET, OPTIONS",
				"Access-Control-Allow-Headers":     "x-header-1, x-header-2",
				"Access-Control-Expose-Headers":    "x-header-3, x-header-4",
				"Access-Control-Max-Age":           "3600",
				"Access-Control-Allow-Credentials": "true",
			},
		},
		{
			name: "preflight wildcard origin with nested labels", cors: multi,
			req:     corsRequest(http.MethodOptions, "https://xpto.www.bar.com", preflight),
			status:  http.StatusNoContent,
			headers: map[string]string{"Access-Control-Allow-Origin": "https://xpto.www.bar.com"},
		},
		{
			name: "wildcard does not match the apex or another scheme", cors: multi,
			req:    corsRequest(http.MethodOptions, "http://www.bar.com", preflight),
			status: http.StatusNoContent,
			absent: []string{"Access-Control-Allow-Origin"},
		},
		{
			name: "preflight non matching origin", cors: multi,
			req:    corsRequest(http.MethodOptions, "https://foobar.com", preflight),
			status: http.StatusNoContent,
			absent: []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods"},
		},
		{
			name: "simple request", cors: multi,
			req:     corsRequest(http.MethodGet, "https://www.foo.com", nil),
			status:  http.StatusOK,
			headers: map[string]string{"Access-Control-Allow-Origin": "https://www.foo.com", "Access-Control-Expose-Headers": "x-header-3, x-header-4"},
		},
		{
			name: "simple request non matching origin reaches the backend", cors: multi,
			req:    corsRequest(http.MethodGet, "https://foobar.com", nil),
			status: http.StatusOK,
			absent: []string{"Access-Control-Allow-Origin"},
		},
		{
			name: "wildcards with credentials echo the request", cors: wildcard,
			req:    corsRequest(http.MethodOptions, "https://other.foo.com:12345", map[string]string{"Access-Control-Request-Method": "PUT", "Access-Control-Request-Headers": "x-a"}),
			status: http.StatusNoContent,
			headers: map[string]string{
				"Access-Control-Allow-Origin":  "https://other.foo.com:12345",
				"Access-Control-Allow-Methods": "PUT",
				"Access-Control-Allow-Headers": "x-a",
				"Access-Control-Max-Age":       "5",
			},
		},
		{
			name: "credentials header is absent without allowCredentials", cors: &CORS{AllowOrigins: []string{"https://www.foo.com"}, AllowMethods: []string{"*"}},
			req:    corsRequest(http.MethodOptions, "https://www.foo.com", map[string]string{"Access-Control-Request-Method": "POST"}),
			status: http.StatusNoContent,
			absent: []string{"Access-Control-Allow-Credentials"},
		},
		{
			name: "options without request method is not a preflight", cors: multi,
			req:    corsRequest(http.MethodOptions, "https://www.foo.com", nil),
			status: http.StatusOK,
			absent: []string{"Access-Control-Allow-Methods"},
		},
		{
			name: "no origin", cors: multi,
			req:     corsRequest(http.MethodGet, "", nil),
			status:  http.StatusOK,
			headers: map[string]string{"Vary": "Origin"},
			absent:  []string{"Access-Control-Allow-Origin"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := &Gateway{Rules: []Rule{{
				Filters:  []Filter{{CORS: tt.cors}},
				Backends: []Backend{{Weight: 1, Middlewares: []string{"cors-backend"}}},
			}}}

			rec := serve(t, g, tt.req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}

			for k, v := range tt.headers {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}

			for _, k := range tt.absent {
				if got := rec.Header().Get(k); got != "" {
					t.Errorf("%s = %q, want absent", k, got)
				}
			}
		})
	}
}

// CORS headers survive interim responses and replace CORS headers of the
// backend instead of adding a second value.
func TestCORSBackendHeaders(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("cors-own", []func(http.Handler) http.Handler{func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// like the reverse proxy on an upstream 103
			clear(w.Header())
			w.WriteHeader(http.StatusEarlyHints)

			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Expose-Headers", "X-Backend")
			w.WriteHeader(http.StatusOK)
		})
	}})

	g := &Gateway{Rules: []Rule{{
		Filters:  []Filter{{CORS: &CORS{AllowOrigins: []string{"https://a.example.com"}, ExposeHeaders: []string{"*"}, AllowCredentials: true}}},
		Backends: []Backend{{Weight: 1, Middlewares: []string{"cors-own"}}},
	}}}

	rec := serve(t, g, corsRequest(http.MethodGet, "https://a.example.com", nil))

	if v := rec.Header().Values("Access-Control-Allow-Origin"); len(v) != 1 || v[0] != "https://a.example.com" {
		t.Fatalf("allow origin = %v", v)
	}

	if v := rec.Header().Values("Vary"); len(v) != 1 || v[0] != "Origin" {
		t.Fatalf("vary = %v", v)
	}

	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentials header missing")
	}

	// "*" is dropped with credentials, an empty header is not sent
	if _, ok := rec.Header()["Access-Control-Expose-Headers"]; ok {
		t.Fatalf("expose headers = %v", rec.Header().Values("Access-Control-Expose-Headers"))
	}
}
