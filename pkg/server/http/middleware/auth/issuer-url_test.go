package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestIssuerURLAppliesInstanceHostReplacement(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuth2: OAuth2Settings{BaseURL: "https://shared.example.com/"},
		// Keep metadata generation on its non-mutating fallback path; signing
		// material is unrelated to this URL contract.
		JWTKey: jwtSetting{PrivateKey: "invalid", KID: "test"},
	})

	m := &Auth{
		PrefixPath: "/auth",
		cache:      cache,
		SessionProvidersConfig: SessionProvidersStatic{HostReplacements: map[string]string{
			"shared.example.com": "site.example.com",
		}},
	}
	r := httptest.NewRequest(http.MethodGet, "https://internal.example.com/auth/oauth2/token", nil)

	if got, want := m.issuerURL(r), "https://site.example.com/auth/oauth2"; got != want {
		t.Fatalf("issuerURL() = %q, want %q", got, want)
	}

	metadata := m.serverMetadata(r, "")
	for _, field := range []string{"issuer", "authorization_endpoint", "token_endpoint", "userinfo_endpoint", "jwks_uri"} {
		value, _ := metadata[field].(string)
		if !strings.HasPrefix(value, "https://site.example.com/auth/oauth2") {
			t.Fatalf("metadata[%q] = %q", field, value)
		}
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
