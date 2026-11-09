package session

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type revokingIssuer struct {
	fakeIssuer
	revoked map[string]bool
	err     error
}

func (f *revokingIssuer) TokenClaimsRevoked(_ context.Context, jti, sid string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}

	return f.revoked[jti] || f.revoked[sid], nil
}

func (f *revokingIssuer) AcceptsIssuer(iss string) bool {
	return iss == "https://auth.example.com/auth/oauth2"
}

func newValidateSession(t *testing.T, key *rsa.PrivateKey, provider Provider) *Session {
	t.Helper()

	issuer := &revokingIssuer{fakeIssuer: fakeIssuer{kid: "kid-1", key: &key.PublicKey}, revoked: map[string]bool{"revoked-jti": true}}
	name := "validate-auth-" + t.Name()
	IssuerRegistry.Set(name, issuer)

	provider.AuthMiddleware = name
	m := &Session{
		Provider: map[string]Provider{"turna": provider},
		Action:   Action{Token: &Token{LoginPath: "/login/"}},
		store:    fakeStore{},
	}
	if err := m.SetAction(); err != nil {
		t.Fatalf("set action: %v", err)
	}

	return m
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":                "https://auth.example.com/auth/oauth2",
		"aud":                DefaultIssuerAudience,
		"typ":                "Bearer",
		"jti":                "jti-1",
		"sid":                "sid-1",
		"preferred_username": "user1",
		"exp":                time.Now().Add(time.Minute).Unix(),
	}
}

func TestValidateAccessToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	m := newValidateSession(t, key, Provider{})

	sign := func(mutate func(jwt.MapClaims)) string {
		c := validClaims()
		mutate(c)
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		token.Header["kid"] = "kid-1"
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		return raw
	}

	cases := map[string]struct {
		mutate func(jwt.MapClaims)
		want   error
	}{
		"valid":           {mutate: func(jwt.MapClaims) {}},
		"missing exp":     {mutate: func(c jwt.MapClaims) { delete(c, "exp") }, want: jwt.ErrTokenRequiredClaimMissing},
		"expired":         {mutate: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, want: jwt.ErrTokenExpired},
		"future iat":      {mutate: func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Hour).Unix() }, want: jwt.ErrTokenUsedBeforeIssued},
		"wrong issuer":    {mutate: func(c jwt.MapClaims) { c["iss"] = "https://evil.example/auth/oauth2" }, want: ErrTokenIssuer},
		"wrong audience":  {mutate: func(c jwt.MapClaims) { c["aud"] = "other-api" }, want: ErrTokenAudience},
		"missing typ":     {mutate: func(c jwt.MapClaims) { delete(c, "typ") }, want: ErrTokenType},
		"refresh token":   {mutate: func(c jwt.MapClaims) { c["typ"] = "Refresh" }, want: ErrTokenType},
		"id token":        {mutate: func(c jwt.MapClaims) { c["typ"] = "ID" }, want: ErrTokenType},
		"revoked ignored": {mutate: func(c jwt.MapClaims) { c["jti"] = "revoked-jti" }},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			claims, provider, err := m.ValidateAccessToken(context.Background(), sign(tc.mutate))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if provider != "turna" || claims.Map["preferred_username"] != "user1" {
					t.Fatalf("provider = %q claims = %v", provider, claims.Map)
				}

				return
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("revocation when enabled", func(t *testing.T) {
		m := newValidateSession(t, key, Provider{CheckRevocation: true})
		_, _, err := m.ValidateAccessToken(context.Background(), sign(func(c jwt.MapClaims) { c["jti"] = "revoked-jti" }))
		if !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("jti err = %v, want revoked", err)
		}

		_, _, err = m.ValidateAccessToken(context.Background(), sign(func(c jwt.MapClaims) { c["sid"] = "revoked-jti" }))
		if !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("sid err = %v, want revoked", err)
		}

		if _, _, err := m.ValidateAccessToken(context.Background(), sign(func(jwt.MapClaims) {})); err != nil {
			t.Fatalf("valid token: %v", err)
		}
	})

	t.Run("revocation store failure is transient", func(t *testing.T) {
		m := newValidateSession(t, key, Provider{CheckRevocation: true})
		IssuerRegistry.Get(m.Provider["turna"].AuthMiddleware).(*revokingIssuer).err = errors.New("redis down")

		_, _, err := m.ValidateAccessToken(context.Background(), sign(func(jwt.MapClaims) {}))
		if !IsUnavailable(err) {
			t.Fatalf("err = %v, want unavailable", err)
		}
	})

	t.Run("explicit issuer and audience", func(t *testing.T) {
		m := newValidateSession(t, key, Provider{Issuer: []string{"https://idp"}, Audience: []string{"api"}})

		if _, _, err := m.ValidateAccessToken(context.Background(), sign(func(c jwt.MapClaims) {
			c["iss"], c["aud"] = "https://idp", []string{"x", "api"}
		})); err != nil {
			t.Fatalf("valid: %v", err)
		}

		if _, _, err := m.ValidateAccessToken(context.Background(), sign(func(jwt.MapClaims) {})); !errors.Is(err, ErrTokenIssuer) {
			t.Fatalf("default issuer err = %v, want issuer error", err)
		}
	})

	t.Run("skip audience check", func(t *testing.T) {
		m := newValidateSession(t, key, Provider{SkipAudienceCheck: true})
		if _, _, err := m.ValidateAccessToken(context.Background(), sign(func(c jwt.MapClaims) { delete(c, "aud") })); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAudienceAcceptedRemote(t *testing.T) {
	provider := Provider{Oauth2: &Oauth2{ClientID: "my-app"}}

	cases := map[string]struct {
		claims map[string]any
		want   bool
	}{
		"aud string":        {claims: map[string]any{"aud": "my-app"}, want: true},
		"aud list":          {claims: map[string]any{"aud": []any{"account", "my-app"}}, want: true},
		"azp (keycloak)":    {claims: map[string]any{"aud": "account", "azp": "my-app"}, want: true},
		"client_id (9068)":  {claims: map[string]any{"aud": "https://api", "client_id": "my-app"}, want: true},
		"cid (okta)":        {claims: map[string]any{"aud": "api://default", "cid": "my-app"}, want: true},
		"appid (azure v1)":  {claims: map[string]any{"aud": "00000003", "appid": "my-app"}, want: true},
		"other application": {claims: map[string]any{"aud": "other-app", "azp": "other-app"}, want: false},
		"no audience":       {claims: map[string]any{}, want: false},
	}

	for name, tc := range cases {
		if got := audienceAccepted(provider, tc.claims); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}

	if audienceAccepted(Provider{Oauth2: &Oauth2{}}, map[string]any{"aud": ""}) {
		t.Error("provider without client_id must not accept tokens")
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer abc":     "abc",
		"bearer abc":     "abc",
		"BEARER  abc ":   "abc",
		"Bearerabc":      "",
		"Basic dXNlcg==": "",
		"":               "",
	}

	for header, want := range cases {
		if got := BearerToken(header); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestBearerLowercaseScheme(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	m := newSkipSession(t, key, nil, false)

	var gotUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User")
	})

	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.Header.Set("Authorization", "bearer "+signSkipToken(t, key, jwt.MapClaims{"preferred_username": "user1"}))
	rec := httptest.NewRecorder()
	m.Do(next, rec, r)

	if rec.Code != http.StatusOK || gotUser != "user1" {
		t.Fatalf("status = %d X-User = %q", rec.Code, gotUser)
	}
}
