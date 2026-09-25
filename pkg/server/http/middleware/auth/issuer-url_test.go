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

func TestSchemelessBaseURLAppliesInstanceHostReplacement(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		OAuth2: OAuth2Settings{BaseURL: "shared.example.com"},
		JWTKey: jwtSetting{PrivateKey: "invalid", KID: "test"},
	})

	m := &Auth{
		PrefixPath: "/auth",
		cache:      cache,
		SessionProvidersConfig: SessionProvidersStatic{HostReplacements: map[string]string{
			"shared.example.com": "site.example.com",
		}},
	}
	r := httptest.NewRequest(http.MethodGet, "http://internal.example.com/auth/oauth2/token", nil)

	if got, want := m.issuerURL(r), "https://site.example.com/auth/oauth2"; got != want {
		t.Fatalf("issuerURL() = %q, want %q", got, want)
	}

	code, err := m.codeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	callback, err := code.AuthCodeRedirectURL(r.Clone(r.Context()), "google")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://site.example.com/auth/oauth2/code/google"; callback != want {
		t.Fatalf("callback = %q, want %q", callback, want)
	}
}

func TestOAuthSurfaceReportsRuntimeCallback(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{})

	m := &Auth{PrefixPath: "/auth", cache: cache}
	// No TLS or forwarded proto: the issuer falls back to http while upstream
	// callbacks use oauth2.schema (default https). The published pattern must
	// be the callback upstream providers actually receive.
	r := httptest.NewRequest(http.MethodGet, "http://login.example.com/auth/v1/info", nil)

	surface := m.oauthSurface(r)
	if got, want := surface["callback_url_pattern"], "https://login.example.com/auth/oauth2/code/{provider}"; got != want {
		t.Fatalf("callback_url_pattern = %q, want %q", got, want)
	}
	if got, want := r.URL.String(), "http://login.example.com/auth/v1/info"; got != want {
		t.Fatalf("request URL mutated to %q", got)
	}
}

func TestPutSettingRejectsInvalidOAuth2BaseURL(t *testing.T) {
	m := &Auth{PrefixPath: "/auth"}

	for _, value := range []string{
		`{"base_url":"ftp://auth.example.com"}`,
		`{"base_url":"https://auth.example.com/sub"}`,
		`{"base_url":"https://auth.example.com?x=1"}`,
		`{"base_url":"https://"}`,
		`{"schema":"ftp"}`,
	} {
		r := httptest.NewRequest(http.MethodPut, "/auth/v1/settings/oauth2", strings.NewReader(`{"value":`+value+`}`))
		r.SetPathValue("namespace", "oauth2")
		w := httptest.NewRecorder()
		m.PutSetting(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", value, w.Code)
		}
	}
}

func TestNormalizeOAuthBaseURL(t *testing.T) {
	for _, tc := range []struct{ raw, schema, want string }{
		{"", "", ""},
		{"https://auth.example.com/", "", "https://auth.example.com"},
		{" auth.example.com ", "", "https://auth.example.com"},
		{"auth.example.com:8080", "http", "http://auth.example.com:8080"},
		{"HTTP://Auth.example.com", "", "http://Auth.example.com"},
	} {
		got, err := normalizeOAuthBaseURL(tc.raw, tc.schema)
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("%q = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestAcceptsIssuerAnyPublicHost(t *testing.T) {
	m := &Auth{PrefixPath: "/auth"}

	for iss, want := range map[string]bool{
		"https://main.example.com/auth/oauth2":      true,
		"http://other.example.com:8080/auth/oauth2": true,
		"https://main.example.com/other/oauth2":     false,
		"https://main.example.com/auth/oauth2/x":    false,
		"ftp://main.example.com/auth/oauth2":        false,
		"https:///auth/oauth2":                      false,
		"":                                          false,
	} {
		if got := m.AcceptsIssuer(iss); got != want {
			t.Fatalf("AcceptsIssuer(%q) = %v, want %v", iss, got, want)
		}
	}
}
