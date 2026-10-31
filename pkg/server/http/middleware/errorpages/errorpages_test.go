package errorpages

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

// upstream answers with the status in the path, e.g. /500.
func upstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			code = http.StatusOK
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"upstream":true}`))
	})
}

func setup(t *testing.T, m *Errors) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(upstream())
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestPages(t *testing.T) {
	h := setup(t, &Errors{
		Statuses: []string{"404", "500-599"},
		Pages: map[string]string{
			"404":     "<h1>{{ .Path }} not found</h1>",
			"500-599": "<h1>{{ .Status }} {{ .StatusText }}</h1>",
		},
	})

	rec := get(h, "/404")
	if rec.Code != http.StatusNotFound || rec.Body.String() != "<h1>/404 not found</h1>" {
		t.Fatalf("404: %d %q", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type %q", ct)
	}

	if rec.Header().Get("X-Upstream") != "yes" {
		t.Fatal("upstream headers dropped")
	}

	if rec := get(h, "/503"); rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "<h1>503 Service Unavailable</h1>" {
		t.Fatalf("503: %d %q", rec.Code, rec.Body.String())
	}

	if rec := get(h, "/401"); rec.Code != http.StatusUnauthorized || rec.Body.String() != `{"upstream":true}` {
		t.Fatalf("401 passed through: %d %q", rec.Code, rec.Body.String())
	}

	if rec := get(h, "/ok"); rec.Code != http.StatusOK || rec.Body.String() != `{"upstream":true}` {
		t.Fatalf("200: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDir(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"404.html": "not found", "5xx.html": "server {{ .Status }}", "error.html": "generic"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	h := setup(t, &Errors{Statuses: []string{"400-599"}, Dir: dir})

	for path, want := range map[string]string{"/404": "not found", "/502": "server 502", "/418": "generic"} {
		if rec := get(h, path); rec.Body.String() != want {
			t.Fatalf("%s: %q", path, rec.Body.String())
		}
	}
}

func TestService(t *testing.T) {
	registry.GlobalReg.AddHttpMiddleware("errors-test-pages", []func(http.Handler) http.Handler{
		func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("page for " + r.URL.Path))
			})
		},
	})

	h := setup(t, &Errors{Statuses: []string{"500-599"}, Service: []string{"errors-test-pages"}, Query: "/errors/{status}.html"})

	rec := get(h, "/502")
	if rec.Code != http.StatusBadGateway || rec.Body.String() != "page for /errors/502.html" || rec.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("%d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestEscapesHTML(t *testing.T) {
	mw, err := (&Errors{
		Statuses: []string{"500-599"},
		Pages:    map[string]string{"500-599": "<p>{{ .Path }} {{ .Host }}</p>"},
	}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = "/<script>alert(1)</script>"
	req.Host = `"><img src=x onerror=alert(1)>`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "<script>") || strings.Contains(rec.Body.String(), "<img") {
		t.Fatalf("not escaped: %d %q", rec.Code, rec.Body.String())
	}
}

func TestPlainText(t *testing.T) {
	h := setup(t, &Errors{
		Statuses:    []string{"500-599"},
		ContentType: "text/plain",
		Pages:       map[string]string{"500-599": "failed: {{ .Path }}"},
	})

	if rec := get(h, "/503?a=<b>"); rec.Body.String() != "failed: /503" {
		t.Fatalf("text page: %q", rec.Body.String())
	}
}

func TestMostSpecificPage(t *testing.T) {
	for range 20 {
		h := setup(t, &Errors{
			Statuses: []string{"400-599"},
			Pages:    map[string]string{"400-599": "any", "404": "exact", "400-499": "client"},
		})

		for path, want := range map[string]string{"/404": "exact", "/403": "client", "/502": "any"} {
			if rec := get(h, path); rec.Body.String() != want {
				t.Fatalf("%s: %q", path, rec.Body.String())
			}
		}
	}
}

func TestDroppedHeaders(t *testing.T) {
	mw, err := (&Errors{Statuses: []string{"404"}, Pages: map[string]string{"404": "x"}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="a.bin"`)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Www-Authenticate", "Bearer")
		w.WriteHeader(http.StatusNotFound)
	}))

	rec := get(h, "/")
	if rec.Header().Get("Content-Disposition") != "" || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("headers kept %v", rec.Header())
	}

	if rec.Header().Get("Www-Authenticate") != "Bearer" {
		t.Fatal("upstream header dropped")
	}
}

func TestInvalid(t *testing.T) {
	for _, m := range []*Errors{
		{},
		{Statuses: []string{"404"}},
		{Statuses: []string{"abc"}, Pages: map[string]string{"404": "x"}},
		{Statuses: []string{"404"}, Pages: map[string]string{"404": "{{ .Broken"}},
		{Statuses: []string{"404"}, Service: []string{"errors-test-missing"}},
		{Statuses: []string{"304"}, Pages: map[string]string{"304": "x"}},
		{Statuses: []string{""}, Pages: map[string]string{"404": "x"}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v accepted", m)
		}
	}
}
