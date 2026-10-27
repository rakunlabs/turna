package canary

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

func echo(name string) {
	registry.GlobalReg.AddHttpMiddleware(name, []func(http.Handler) http.Handler{
		func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Served", name+"|"+r.Header.Get("X-Variant"))
			})
		},
	})
}

func setup(t *testing.T, m *Canary) http.Handler {
	t.Helper()

	echo("canary-stable")
	echo("canary-beta")

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(nil)
}

func variants(stable, beta int) []Variant {
	return []Variant{
		{Name: "stable", Weight: stable, Middlewares: []string{"canary-stable"}},
		{Name: "beta", Weight: beta, Middlewares: []string{"canary-beta"}},
	}
}

func served(h http.Handler, req *http.Request) (string, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Header().Get("X-Served"), rec
}

func TestWeights(t *testing.T) {
	h := setup(t, &Canary{Variants: variants(90, 10), VariantHeader: "X-Variant"})

	counts := map[string]int{}
	for range 2000 {
		s, _ := served(h, httptest.NewRequest(http.MethodGet, "/", nil))
		counts[s]++
	}

	beta := counts["canary-beta|beta"]
	if beta < 100 || beta > 320 {
		t.Fatalf("beta share = %d of 2000, counts %v", beta, counts)
	}

	if counts["canary-stable|stable"]+beta != 2000 {
		t.Fatalf("counts %v", counts)
	}
}

func TestZeroWeight(t *testing.T) {
	h := setup(t, &Canary{Variants: variants(100, 0)})

	for range 100 {
		if s, _ := served(h, httptest.NewRequest(http.MethodGet, "/", nil)); s != "canary-stable|" {
			t.Fatalf("served %q", s)
		}
	}
}

func TestOverride(t *testing.T) {
	h := setup(t, &Canary{Variants: variants(100, 0), OverrideHeader: "X-Canary", OverrideCookie: "canary"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Canary", "beta")
	if s, _ := served(h, req); s != "canary-beta|" {
		t.Fatalf("header override served %q", s)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "canary", Value: "beta"})
	if s, _ := served(h, req); s != "canary-beta|" {
		t.Fatalf("cookie override served %q", s)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Canary", "unknown")
	if s, _ := served(h, req); s != "canary-stable|" {
		t.Fatalf("unknown override served %q", s)
	}
}

func TestStickyCookie(t *testing.T) {
	h := setup(t, &Canary{Variants: variants(50, 50), Sticky: &Sticky{}})

	first, rec := served(h, httptest.NewRequest(http.MethodGet, "/", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "turna_canary" || !cookies[0].HttpOnly {
		t.Fatalf("cookies %v", cookies)
	}

	for range 50 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(cookies[0])

		s, rec := served(h, req)
		if s != first {
			t.Fatalf("sticky served %q, first %q", s, first)
		}

		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("cookie set again")
		}
	}
}

func TestStickyKey(t *testing.T) {
	h := setup(t, &Canary{Variants: variants(50, 50), Sticky: &Sticky{Key: []string{"header:X-User"}}})

	seen := map[string]bool{}
	for u := range 50 {
		var first string

		for i := range 5 {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-User", strconv.Itoa(u))

			s, _ := served(h, req)
			if i == 0 {
				first = s
			} else if s != first {
				t.Fatalf("user %d moved from %q to %q", u, first, s)
			}
		}

		seen[first] = true
	}

	if len(seen) != 2 {
		t.Fatalf("users all on %v", seen)
	}
}

func TestInvalidConfig(t *testing.T) {
	echo("canary-stable")

	for _, m := range []*Canary{
		{Variants: variants(1, 1)[:1]},
		{Variants: variants(0, 0)},
		{Variants: []Variant{{Name: "a", Weight: 1, Middlewares: []string{"canary-stable"}}, {Name: "a", Weight: 1, Middlewares: []string{"canary-stable"}}}},
		{Variants: []Variant{{Name: "a", Weight: 1, Middlewares: []string{"canary-stable"}}, {Name: "b", Weight: 1, Middlewares: []string{"missing"}}}},
		{Variants: []Variant{{Name: "a", Weight: -1, Middlewares: []string{"canary-stable"}}, {Name: "b", Weight: 1, Middlewares: []string{"canary-stable"}}}},
		{Variants: []Variant{{Name: "a", Weight: 1}, {Name: "b", Weight: 1, Middlewares: []string{"canary-stable"}}}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
