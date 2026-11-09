package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

func TestLoginAuthenticationMethodSurvivesRefresh(t *testing.T) {
	m := &Session{
		CookieName: "auth_session",
		Store: Store{File: &store.File{
			SessionKey: "test-session-key",
			Path:       t.TempDir(),
		}},
	}
	if err := m.SetStore(context.Background()); err != nil {
		t.Fatalf("SetStore: %v", err)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "https://example.com/login", nil)
	loginResponse := httptest.NewRecorder()
	if err := m.SetLoginToken(loginResponse, loginRequest, []byte(`{"access_token":"first"}`), "auth", AuthenticationMethodCode); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	loginCookies := loginResponse.Result().Cookies()
	if len(loginCookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(loginCookies))
	}
	refreshRequest := httptest.NewRequest(http.MethodPost, "https://example.com/refresh", nil)
	refreshRequest.AddCookie(loginCookies[0])
	if method, err := m.GetAuthenticationMethod(refreshRequest); err != nil || method != AuthenticationMethodCode {
		t.Fatalf("method after login = %q, err=%v", method, err)
	}

	refreshResponse := httptest.NewRecorder()
	if err := m.SetToken(refreshResponse, refreshRequest, []byte(`{"access_token":"refreshed"}`), "auth"); err != nil {
		t.Fatalf("SetToken refresh: %v", err)
	}
	refreshCookies := refreshResponse.Result().Cookies()
	if len(refreshCookies) != 1 {
		t.Fatalf("refresh cookies = %d, want 1", len(refreshCookies))
	}

	checkRequest := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	checkRequest.AddCookie(refreshCookies[0])
	if method, err := m.GetAuthenticationMethod(checkRequest); err != nil || method != AuthenticationMethodCode {
		t.Fatalf("method after refresh = %q, err=%v", method, err)
	}
}

func TestSetStoreSessionKey(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		file      string
		redis     string
		wantFile  string
		wantRedis string
	}{
		{
			name:      "inherit top-level key",
			session:   "shared-secret",
			wantFile:  "shared-secret",
			wantRedis: "shared-secret",
		},
		{
			name:      "store-specific keys take precedence",
			session:   "shared-secret",
			file:      "file-secret",
			redis:     "redis-secret",
			wantFile:  "file-secret",
			wantRedis: "redis-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Session{
				SessionKey: tt.session,
				Store: Store{
					Active: "file",
					File: &store.File{
						SessionKey: tt.file,
						Path:       t.TempDir(),
					},
					Redis: &store.Redis{SessionKey: tt.redis},
				},
			}

			if err := m.SetStore(context.Background()); err != nil {
				t.Fatalf("SetStore() error = %v", err)
			}

			if got := m.Store.File.SessionKey; got != tt.wantFile {
				t.Errorf("file session key = %q, want %q", got, tt.wantFile)
			}
			if got := m.Store.Redis.SessionKey; got != tt.wantRedis {
				t.Errorf("redis session key = %q, want %q", got, tt.wantRedis)
			}
		})
	}
}

func TestLoginRotatesSessionID(t *testing.T) {
	m := &Session{
		CookieName: "auth_session",
		Store: Store{File: &store.File{
			SessionKey: "test-session-key",
			Path:       t.TempDir(),
		}},
	}
	if err := m.SetStore(context.Background()); err != nil {
		t.Fatalf("SetStore: %v", err)
	}

	first := httptest.NewRecorder()
	if err := m.SetLoginToken(first, httptest.NewRequest(http.MethodPost, "https://example.com/login", nil), []byte(`{"access_token":"first"}`), "auth", AuthenticationMethodCode); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}
	oldCookie := first.Result().Cookies()[0]

	loginRequest := httptest.NewRequest(http.MethodPost, "https://example.com/login", nil)
	loginRequest.AddCookie(oldCookie)
	second := httptest.NewRecorder()
	if err := m.SetLoginToken(second, loginRequest, []byte(`{"access_token":"second"}`), "auth", AuthenticationMethodPassword); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}
	newCookie := second.Result().Cookies()[0]

	if newCookie.Value == oldCookie.Value {
		t.Fatal("login reused the incoming session ID")
	}

	oldRequest := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	oldRequest.AddCookie(oldCookie)
	if v, err := m.store.Get(oldRequest, m.CookieName); err != nil || !v.IsNew {
		t.Fatalf("previous session still valid: isNew=%v err=%v", v.IsNew, err)
	}

	newRequest := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	newRequest.AddCookie(newCookie)
	if method, err := m.GetAuthenticationMethod(newRequest); err != nil || method != AuthenticationMethodPassword {
		t.Fatalf("method = %q, err=%v", method, err)
	}
}

func TestSessionCookieDefaults(t *testing.T) {
	falseV := false

	cases := map[string]struct {
		options  Options
		secure   bool
		httpOnly bool
		sameSite http.SameSite
		err      bool
	}{
		"defaults": {secure: true, httpOnly: true, sameSite: http.SameSiteLaxMode},
		"explicit insecure for local http": {
			options:  Options{Secure: &falseV, HttpOnly: &falseV, SameSite: http.SameSiteStrictMode},
			sameSite: http.SameSiteStrictMode,
		},
		"same_site none needs secure": {options: Options{Secure: &falseV, SameSite: http.SameSiteNoneMode}, err: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := &Session{
				CookieName: "auth_session",
				Options:    tc.options,
				Store:      Store{File: &store.File{SessionKey: "k", Path: t.TempDir()}},
			}

			err := m.SetStore(context.Background())
			if tc.err {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}
			if err != nil {
				t.Fatalf("SetStore: %v", err)
			}

			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "https://example.com/login", nil)
			if err := m.SetLoginToken(rec, r, []byte(`{"access_token":"a"}`), "auth", AuthenticationMethodCode); err != nil {
				t.Fatalf("SetLoginToken: %v", err)
			}

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %d", len(cookies))
			}
			c := cookies[0]
			if c.Secure != tc.secure || c.HttpOnly != tc.httpOnly || c.SameSite != tc.sameSite {
				t.Fatalf("secure=%v httpOnly=%v sameSite=%v", c.Secure, c.HttpOnly, c.SameSite)
			}
		})
	}
}
