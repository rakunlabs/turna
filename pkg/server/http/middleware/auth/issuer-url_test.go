package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssuerURLCanonicalBaseURL(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuth2: OAuth2Settings{BaseURL: "https://auth.example.com/"}})

	m := &Auth{PrefixPath: "/auth", cache: cache}
	r := httptest.NewRequest(http.MethodGet, "https://app.example.com/auth/oauth2/token", nil)

	if got, want := m.issuerURL(r), "https://auth.example.com/auth/oauth2"; got != want {
		t.Fatalf("issuerURL() = %q, want %q", got, want)
	}
}

func TestIssuerURLRequestFallback(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{})

	m := &Auth{PrefixPath: "/auth", cache: cache}
	r := httptest.NewRequest(http.MethodGet, "http://internal/auth/oauth2/token", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "login.example.com")

	if got, want := m.issuerURL(r), "https://login.example.com/auth/oauth2"; got != want {
		t.Fatalf("issuerURL() = %q, want %q", got, want)
	}
}
