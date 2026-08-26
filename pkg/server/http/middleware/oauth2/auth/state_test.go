package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStateBindingCookie(t *testing.T) {
	const (
		state   = "provider-state"
		binding = "browser-secret"
		path    = "/auth/oauth2/code/"
	)

	w := httptest.NewRecorder()
	SetStateBindingCookie(w, state, binding, path, true, 2*time.Minute)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if !strings.HasPrefix(cookie.Name, stateBindingCookiePrefix) || cookie.Path != path {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 120 {
		t.Fatalf("cookie security attributes = %#v", cookie)
	}

	r := httptest.NewRequest(http.MethodGet, "https://auth.example/auth/oauth2/code/provider", nil)
	r.AddCookie(cookie)
	if !ValidStateBinding(r, state, StateBindingHash(binding)) {
		t.Fatal("matching browser binding was rejected")
	}
	if ValidStateBinding(r, state, StateBindingHash("other-browser")) {
		t.Fatal("mismatched browser binding was accepted")
	}

	clear := httptest.NewRecorder()
	ClearStateBindingCookie(clear, state, path, true)
	cleared := clear.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != cookie.Name || cleared[0].MaxAge >= 0 {
		t.Fatalf("cleared cookie = %#v", cleared)
	}
}

func TestRequestIsHTTPS(t *testing.T) {
	if !RequestIsHTTPS(httptest.NewRequest(http.MethodGet, "https://auth.example/callback", nil)) {
		t.Fatal("direct HTTPS request was not recognized")
	}

	r := httptest.NewRequest(http.MethodGet, "http://auth.example/callback", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if !RequestIsHTTPS(r) {
		t.Fatal("forwarded HTTPS request was not recognized")
	}

	if RequestIsHTTPS(httptest.NewRequest(http.MethodGet, "http://auth.example/callback", nil)) {
		t.Fatal("HTTP request was recognized as HTTPS")
	}
}
