package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFGuard(t *testing.T) {
	t.Parallel()

	m := &Auth{PrefixPath: "/auth"}
	handler := m.csrfGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{
			name:   "server-to-server call without browser headers",
			method: http.MethodPost, path: "/auth/v1/users",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			want:    http.StatusOK,
		},
		{
			name:   "own UI on another origin sending JSON (CORS decides)",
			method: http.MethodPost, path: "/auth/v1/totp/register",
			headers: map[string]string{"Content-Type": "application/json", "Origin": "https://portal.example.com", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusOK,
		},
		{
			name:   "same-origin form post",
			method: http.MethodPost, path: "/auth/v1/device",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusOK,
		},
		{
			name:   "same-origin by Origin header when fetch metadata is missing",
			method: http.MethodPost, path: "/auth/v1/device",
			headers: map[string]string{"Content-Type": "text/plain", "Origin": "https://idp.example.com"},
			want:    http.StatusOK,
		},
		{
			name:   "cross-site form post to approve a device code",
			method: http.MethodPost, path: "/auth/v1/device",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name:   "cross-site text/plain post (no preflight) without fetch metadata",
			method: http.MethodPost, path: "/auth/v1/totp",
			headers: map[string]string{"Content-Type": "text/plain", "Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name:   "sandboxed iframe with null origin",
			method: http.MethodPost, path: "/auth/v1/me/password",
			headers: map[string]string{"Content-Type": "multipart/form-data; boundary=x", "Origin": "null"},
			want:    http.StatusForbidden,
		},
		{
			name:   "cross-site delete always needs a preflight",
			method: http.MethodDelete, path: "/auth/v1/totp",
			headers: map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusOK,
		},
		{
			name:   "token endpoint form post from another site is by design",
			method: http.MethodPost, path: "/auth/oauth2/token",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://app.example", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusOK,
		},
		{
			name:   "SAML ACS is a cross-site form post by design",
			method: http.MethodPost, path: "/auth/saml/idp/acs",
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "https://idp.example", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(tt.method, "https://idp.example.com"+tt.path, strings.NewReader("x=1"))
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestConsentPageNotFramable(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	(&Auth{}).renderConsent(w, http.StatusOK, consentPageData{Flow: "f", ConsentToken: "tok"})

	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("consent page must not be framable")
	}
	if !strings.Contains(w.Body.String(), `name="consent_token" value="tok"`) {
		t.Error("consent form misses the consent token")
	}
}
