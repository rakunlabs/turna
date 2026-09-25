package csrf

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func newHandler(t *testing.T, m *CSRF) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	return mw(okHandler)
}

func TestOriginCheck(t *testing.T) {
	h := newHandler(t, &CSRF{
		TrustedOrigins: []string{"https://trusted.example.com"},
		BypassPatterns: []string{"POST /webhook/"},
	})

	tests := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{"safe method cross-site", http.MethodGet, "/", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
		{"same-origin post", http.MethodPost, "/", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"cross-site post", http.MethodPost, "/", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"origin mismatch", http.MethodPost, "/", map[string]string{"Origin": "https://evil.com"}, http.StatusForbidden},
		{"trusted origin", http.MethodPost, "/", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://trusted.example.com"}, http.StatusOK},
		{"non-browser", http.MethodPost, "/", nil, http.StatusOK},
		{"bypass", http.MethodPost, "/webhook/github", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://example.com"+tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func getCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" {
			return c
		}
	}

	t.Fatal("csrf cookie not set")

	return nil
}

func TestToken(t *testing.T) {
	for _, secret := range []string{"", "s3cret"} {
		t.Run("secret="+secret, func(t *testing.T) {
			h := newHandler(t, &CSRF{Token: Token{Enabled: true, Secret: secret}})

			cookie := getCookie(t, h)
			if !cookie.Secure || cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("unexpected cookie attributes %+v", cookie)
			}

			post := func(token string, form bool) int {
				var req *http.Request
				if form {
					req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				} else {
					req = httptest.NewRequest(http.MethodPost, "/", nil)
					if token != "" {
						req.Header.Set("X-CSRF-Token", token)
					}
				}
				req.AddCookie(cookie)

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)

				return rec.Code
			}

			if got := post(cookie.Value, false); got != http.StatusOK {
				t.Fatalf("header token status = %d", got)
			}

			if got := post(cookie.Value, true); got != http.StatusOK {
				t.Fatalf("form token status = %d", got)
			}

			if got := post("", false); got != http.StatusForbidden {
				t.Fatalf("missing token status = %d", got)
			}

			if got := post("wrong", false); got != http.StatusForbidden {
				t.Fatalf("wrong token status = %d", got)
			}
		})
	}
}

func TestTokenForgedCookie(t *testing.T) {
	h := newHandler(t, &CSRF{Token: Token{Enabled: true, Secret: "s3cret"}})

	forged := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.invalid"

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: forged})
	req.Header.Set("X-CSRF-Token", forged)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestInvalidConfig(t *testing.T) {
	f := false

	for _, m := range []*CSRF{
		{TrustedOrigins: []string{"not a url"}},
		{BypassPatterns: []string{"BAD PATTERN WITH SPACES"}},
		{Token: Token{Enabled: true, CookieSameSite: "weird"}},
		{Token: Token{Enabled: true, CookieSameSite: "none", CookieSecure: &f}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("expected error for %+v", m)
		}
	}
}
