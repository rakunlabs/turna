package maintenance

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T, m *Maintenance) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func request(h http.Handler, path, remote string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if remote != "" {
		req.RemoteAddr = remote
	}

	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestDisabled(t *testing.T) {
	h := setup(t, &Maintenance{})

	if rec := request(h, "/", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestEnabled(t *testing.T) {
	h := setup(t, &Maintenance{
		Enabled:      true,
		RetryAfter:   90 * time.Second,
		Allow:        []string{"10.0.0.0/8"},
		AllowPaths:   []string{"/health"},
		BypassHeader: "X-Bypass",
		BypassValue:  "secret",
	})

	rec := request(h, "/", "203.0.113.1:1")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "90" {
		t.Fatalf("status = %d, retry %q", rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"ip":     request(h, "/", "10.1.2.3:1"),
		"path":   request(h, "/health/live", "203.0.113.1:1"),
		"header": request(h, "/", "203.0.113.1:1", "X-Bypass", "secret"),
	} {
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s bypass status = %d", name, rec.Code)
		}
	}

	if rec := request(h, "/", "203.0.113.1:1", "X-Bypass", "wrong"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("wrong bypass value status = %d", rec.Code)
	}
}

func TestHTMLMessage(t *testing.T) {
	h := setup(t, &Maintenance{Enabled: true, Message: "<h1>Back soon</h1>"})

	rec := request(h, "/", "")
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" || rec.Body.String() != "<h1>Back soon</h1>" {
		t.Fatalf("got %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance")
	h := setup(t, &Maintenance{File: path, CheckInterval: time.Millisecond})

	if rec := request(h, "/", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d without file", rec.Code)
	}

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	if rec := request(h, "/", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d with file", rec.Code)
	}

	_ = os.Remove(path)
	time.Sleep(5 * time.Millisecond)

	if rec := request(h, "/", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d after removal", rec.Code)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*Maintenance{
		{RetryAfter: -time.Second},
		{BypassHeader: "X-Bypass"},
		{Allow: []string{"bad"}},
		{TrustedProxies: []string{"bad"}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
