package secureheaders

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, m *SecureHeaders, req *http.Request, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	if h == nil {
		h = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	}

	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, req)

	return rec
}

func TestDefaults(t *testing.T) {
	rec := serve(t, &SecureHeaders{}, httptest.NewRequest(http.MethodGet, "/", nil), nil)

	want := map[string]string{
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "",
		"Content-Security-Policy":   "",
	}

	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestDisableAndKeep(t *testing.T) {
	m := &SecureHeaders{FrameOptions: "-", RemoveHeaders: []string{"Server"}}

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/", nil), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "nginx")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.WriteHeader(http.StatusOK)
	})

	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("disabled header set")
	}
	if rec.Header().Get("Server") != "" {
		t.Error("Server not removed")
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("upstream header replaced: %q", got)
	}

	m.Override = true
	rec = serve(t, m, httptest.NewRequest(http.MethodGet, "/", nil), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
	})
	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Errorf("override: %q", got)
	}
}

func TestHSTS(t *testing.T) {
	m := &SecureHeaders{HSTS: &HSTS{IncludeSubdomains: true, Preload: true, TrustForwardedProto: true}}
	want := "max-age=31536000; includeSubDomains; preload"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := serve(t, m, req, nil).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("plain http got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	if got := serve(t, m, req, nil).Header().Get("Strict-Transport-Security"); got != want {
		t.Errorf("tls got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := serve(t, m, req, nil).Header().Get("Strict-Transport-Security"); got != want {
		t.Errorf("forwarded got %q", got)
	}
}

func TestNonce(t *testing.T) {
	m := &SecureHeaders{
		ContentSecurityPolicy: "script-src 'nonce-{nonce}'",
		NonceHeader:           "X-CSP-Nonce",
	}

	var seen string
	h := func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Get("X-CSP-Nonce") }

	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/", nil), h)
	csp := rec.Header().Get("Content-Security-Policy")

	if seen == "" || csp != "script-src 'nonce-"+seen+"'" {
		t.Fatalf("csp %q, nonce %q", csp, seen)
	}

	first := seen
	serve(t, m, httptest.NewRequest(http.MethodGet, "/", nil), h)
	if seen == first {
		t.Fatal("nonce reused")
	}
}

func TestReportOnly(t *testing.T) {
	m := &SecureHeaders{ContentSecurityPolicy: "default-src 'self'", ContentSecurityPolicyReportOnly: true}
	rec := serve(t, m, httptest.NewRequest(http.MethodGet, "/", nil), nil)

	if !strings.Contains(rec.Header().Get("Content-Security-Policy-Report-Only"), "self") ||
		rec.Header().Get("Content-Security-Policy") != "" {
		t.Fatal("report only header not set")
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*SecureHeaders{
		{NonceHeader: "X-Nonce"},
		{HSTS: &HSTS{Preload: true}},
		{HSTS: &HSTS{MaxAge: -time.Second}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
