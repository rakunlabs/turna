package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/ada"
)

func TestRuleRouterNestedWildcardRoute(t *testing.T) {
	for _, specificFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "specific first", false: "fallback first"}[specificFirst], func(t *testing.T) {
			inner := ada.NewMux()
			ok := func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				w.WriteHeader(stdhttp.StatusNoContent)
			}
			inner.GET("/auth", ok)
			inner.GET("/auth/", ok)
			inner.GET("/auth/v1/info", ok)
			middleware := func(next stdhttp.Handler) stdhttp.Handler {
				inner.NotFound(next.ServeHTTP)
				return inner
			}

			router := NewRuleRouter()
			selection := RuleSelection{Entrypoint: "web"}
			router.SetRule(selection)
			mux := router.GetMux(selection)
			registerSpecific := func() {
				mux.Handle("/auth/*", middleware(stdhttp.NotFoundHandler()))
			}
			registerFallback := func() {
				mux.Handle("/*", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
					w.WriteHeader(stdhttp.StatusTeapot)
				}))
			}
			if specificFirst {
				registerSpecific()
				registerFallback()
			} else {
				registerFallback()
				registerSpecific()
			}

			for _, tc := range []struct {
				path string
				code int
			}{
				{path: "/auth", code: stdhttp.StatusTeapot},
				{path: "/auth/", code: stdhttp.StatusNoContent},
				{path: "/auth/v1/info", code: stdhttp.StatusNoContent},
			} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(stdhttp.MethodGet, tc.path, nil)
				router.Serve("web").ServeHTTP(rec, req)

				if rec.Code != tc.code {
					t.Fatalf("%s status = %d, want %d; body=%q", tc.path, rec.Code, tc.code, rec.Body.String())
				}
			}
		})
	}
}

func TestRuleRouterRootWildcardIncludesRoot(t *testing.T) {
	router := NewRuleRouter()
	selection := RuleSelection{Entrypoint: "web"}
	router.SetRule(selection)
	router.GetMux(selection).Handle("/*", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	router.Serve("web").ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, stdhttp.StatusNoContent, rec.Body.String())
	}
}
