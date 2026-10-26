package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/request"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

// unsignedJWT builds a token for claims that are only read unverified.
func unsignedJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	return raw
}

func tokenBody(t *testing.T, access, refresh string) []byte {
	t.Helper()

	body, err := json.Marshal(TokenData{AccessToken: access, RefreshToken: refresh})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return body
}

func TestSessionLifetime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(30 * 24 * time.Hour)

	standardExp := now.Add(24 * time.Hour)

	remembered := unsignedJWT(t, jwt.MapClaims{"exp": exp.Unix(), "remember_me": true})
	standard := unsignedJWT(t, jwt.MapClaims{"exp": standardExp.Unix(), "remember_me": false})
	external := unsignedJWT(t, jwt.MapClaims{"exp": now.Add(10 * time.Hour).Unix()})
	expired := unsignedJWT(t, jwt.MapClaims{"exp": now.Add(-time.Minute).Unix(), "remember_me": true})

	tests := []struct {
		name       string
		body       []byte
		wantOK     bool
		wantMaxAge int
		wantExp    time.Time
	}{
		{"remembered gets persistent cookie up to refresh expiry", tokenBody(t, "a", remembered), true, 30 * 24 * 3600, exp},
		{"standard keeps a persistent cookie up to refresh expiry", tokenBody(t, "a", standard), true, 24 * 3600, standardExp},
		{"external keeps configured max age", tokenBody(t, "a", external), true, 86400, now.Add(10 * time.Hour)},
		{"opaque refresh token uses refresh_expires_in", []byte(`{"access_token":"a","refresh_token":"opaque","refresh_expires_in":3600}`), true, 86400, now.Add(time.Hour)},
		{"no refresh token", tokenBody(t, "a", ""), false, 0, time.Time{}},
		{"expired refresh token", tokenBody(t, "a", expired), false, 0, time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExp, gotMaxAge, ok := sessionLifetime(tt.body, 86400, now)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if gotMaxAge != tt.wantMaxAge {
				t.Errorf("max age = %d, want %d", gotMaxAge, tt.wantMaxAge)
			}
			if !gotExp.Equal(tt.wantExp) {
				t.Errorf("expires = %v, want %v", gotExp, tt.wantExp)
			}
		})
	}
}

func newFileSession(t *testing.T) *Session {
	t.Helper()

	m := &Session{
		CookieName: "auth_session",
		Store: Store{File: &store.File{
			SessionKey: "test-session-key",
			Path:       t.TempDir(),
		}},
		Action: Action{Token: &Token{}},
	}
	if err := m.SetStore(context.Background()); err != nil {
		t.Fatalf("SetStore: %v", err)
	}

	return m
}

func TestRememberedSessionOutlivesDefaultMaxAge(t *testing.T) {
	m := newFileSession(t)

	refresh := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(30 * 24 * time.Hour).Unix(), "remember_me": true})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/login", nil)
	rec := httptest.NewRecorder()
	if err := m.SetLoginToken(rec, req, tokenBody(t, "a", refresh), "auth", AuthenticationMethodPassword); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	if cookies[0].MaxAge < 29*24*3600 {
		t.Fatalf("remembered cookie max age = %d, want ~30 days", cookies[0].MaxAge)
	}

	check := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	check.AddCookie(cookies[0])
	if _, _, err := m.GetTokenData(check); err != nil {
		t.Fatalf("session not readable: %v", err)
	}
}

// A standard session must survive a browser restart for the rest of its
// refresh window, like the previous fixed 24h cookie did.
func TestStandardSessionSurvivesBrowserRestart(t *testing.T) {
	m := newFileSession(t)

	refresh := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(24 * time.Hour).Unix(), "remember_me": false})

	rec := httptest.NewRecorder()
	if err := m.SetLoginToken(rec, httptest.NewRequest(http.MethodPost, "/", nil), tokenBody(t, "a", refresh), "auth", AuthenticationMethodPassword); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge < 23*3600 {
		t.Fatalf("standard session must be a persistent ~24h cookie: %+v", cookies)
	}
}

func TestRefreshRejected(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{&request.StatusError{StatusCode: http.StatusBadRequest}, true},
		{&request.StatusError{StatusCode: http.StatusUnauthorized}, true},
		{&request.StatusError{StatusCode: http.StatusInternalServerError}, false},
		{&request.StatusError{StatusCode: http.StatusBadGateway}, false},
		{errors.New("dial tcp: connection refused"), false},
		{errProviderNotFound, true},
	}

	for _, tt := range tests {
		if got := refreshRejected(tt.err); got != tt.want {
			t.Errorf("refreshRejected(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

type statusIssuer struct {
	status int
	body   string
}

func (f *statusIssuer) Keyfunc(*jwt.Token) (any, error) { return nil, ErrKIDNotFound }

func (f *statusIssuer) IssueToken(*http.Request, url.Values) ([]byte, int, error) {
	return []byte(f.body), f.status, nil
}

func sessionWithIssuer(t *testing.T, name string, issuer InfIssuer) (*Session, *http.Cookie) {
	t.Helper()

	IssuerRegistry.Set(name, issuer)

	m := newFileSession(t)
	m.Provider = map[string]Provider{"turna": {AuthMiddleware: name, Oauth2: &Oauth2{ClientID: "ui"}}}

	expiredAccess := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()})
	refresh := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "remember_me": true})

	rec := httptest.NewRecorder()
	if err := m.SetLoginToken(rec, httptest.NewRequest(http.MethodPost, "/", nil), tokenBody(t, expiredAccess, refresh), "turna", AuthenticationMethodPassword); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}

	return m, rec.Result().Cookies()[0]
}

func TestRefreshSessionTransientFailureKeepsSession(t *testing.T) {
	m, cookie := sessionWithIssuer(t, "transient-auth", &statusIssuer{status: http.StatusInternalServerError, body: `{"error":"server_error"}`})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	token, _, err := m.GetTokenData(r)
	if err != nil {
		t.Fatalf("GetTokenData: %v", err)
	}

	_, err = m.refreshSession(httptest.NewRecorder(), r, "turna", token)
	if !IsUnavailable(err) {
		t.Fatalf("server_error must be transient, got %v", err)
	}
}

func TestRefreshSessionRejectedIsFinal(t *testing.T) {
	m, cookie := sessionWithIssuer(t, "rejecting-auth", &statusIssuer{status: http.StatusUnauthorized, body: `{"error":"invalid_grant"}`})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	token, _, err := m.GetTokenData(r)
	if err != nil {
		t.Fatalf("GetTokenData: %v", err)
	}

	_, err = m.refreshSession(httptest.NewRecorder(), r, "turna", token)
	if err == nil || IsUnavailable(err) {
		t.Fatalf("invalid_grant must be final, got %v", err)
	}
}

// A refresh that loses the race against another request must pick up the
// token that request stored instead of logging the user out.
func TestRefreshSessionUsesConcurrentlyRotatedToken(t *testing.T) {
	m, cookie := sessionWithIssuer(t, "raced-auth", &statusIssuer{status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(cookie)
	stale, _, err := m.GetTokenData(r)
	if err != nil {
		t.Fatalf("GetTokenData: %v", err)
	}

	// another request already refreshed and stored a fresh token
	freshAccess := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(15 * time.Minute).Unix()})
	freshRefresh := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "remember_me": true})
	if err := m.SetToken(httptest.NewRecorder(), r, tokenBody(t, freshAccess, freshRefresh), "turna"); err != nil {
		t.Fatalf("SetToken: %v", err)
	}

	got, err := m.refreshSession(httptest.NewRecorder(), r, "turna", stale)
	if err != nil {
		t.Fatalf("refresh after concurrent rotation: %v", err)
	}
	if got.AccessToken != freshAccess {
		t.Fatalf("did not adopt the stored token")
	}
}

func TestDoTransientRefreshFailureKeepsCookie(t *testing.T) {
	m, cookie := sessionWithIssuer(t, "do-transient-auth", &statusIssuer{status: http.StatusServiceUnavailable, body: "down"})
	m.Action.Active = actionToken
	m.Action.Token.LoginPath = "/login/"

	r := httptest.NewRequest(http.MethodGet, "/app", nil)
	r.Header.Set("Accept", "text/html")
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()

	m.Do(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next must not run")
	}), rec, r)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookie.Name && c.MaxAge < 0 {
			t.Fatal("transient failure deleted the session cookie")
		}
	}

	// the session is still stored
	check := httptest.NewRequest(http.MethodGet, "/", nil)
	check.AddCookie(cookie)
	if _, _, err := m.GetTokenData(check); err != nil {
		t.Fatalf("session lost: %v", err)
	}
}

func TestFileStoreExpiresRecord(t *testing.T) {
	m := newFileSession(t)

	// a refresh token that is about to expire bounds the stored record
	refresh := unsignedJWT(t, jwt.MapClaims{"exp": time.Now().Add(2 * time.Second).Unix(), "remember_me": true})

	rec := httptest.NewRecorder()
	if err := m.SetLoginToken(rec, httptest.NewRequest(http.MethodPost, "/", nil), tokenBody(t, "a", refresh), "auth", AuthenticationMethodPassword); err != nil {
		t.Fatalf("SetLoginToken: %v", err)
	}
	cookie := rec.Result().Cookies()[0]

	check := httptest.NewRequest(http.MethodGet, "/", nil)
	check.AddCookie(cookie)
	if _, _, err := m.GetTokenData(check); err != nil {
		t.Fatalf("fresh session: %v", err)
	}

	time.Sleep(3 * time.Second)

	if _, _, err := m.GetTokenData(check); !errors.Is(err, errNoSession) {
		t.Fatalf("expired record must read as no session, got %v", err)
	}
}

func TestInvalidCookieIsNoSession(t *testing.T) {
	m := newFileSession(t)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "auth_session", Value: "tampered"})
	if _, _, err := m.GetTokenData(r); !errors.Is(err, errNoSession) {
		t.Fatalf("tampered cookie: %v", err)
	}
}

func TestIsUnavailableWrapsStoreErrors(t *testing.T) {
	if !IsUnavailable(errors.Join(store.ErrUnavailable, errors.New("i/o timeout"))) {
		t.Fatal("store errors must count as unavailable")
	}
	if IsUnavailable(errors.New("token is invalid")) {
		t.Fatal("plain errors are not transient")
	}
}
