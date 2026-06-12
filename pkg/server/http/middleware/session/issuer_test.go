package session

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

type fakeIssuer struct {
	kid string
	key any
}

func (f *fakeIssuer) Keyfunc(token *jwt.Token) (any, error) {
	if kid, _ := token.Header["kid"].(string); kid != f.kid {
		return nil, ErrKIDNotFound
	}

	return f.key, nil
}

func (f *fakeIssuer) IssueToken(_ context.Context, form url.Values) ([]byte, int, error) {
	return []byte(`{"grant_type":"` + form.Get("grant_type") + `"}`), 200, nil
}

func TestIssuerKeyFunc(t *testing.T) {
	IssuerRegistry.Set("test-auth", &fakeIssuer{kid: "kid-1", key: "public-key"})

	keyFunc := &issuerKeyFunc{providers: map[string]string{"turna": "test-auth"}}

	token := &jwt.Token{Header: map[string]any{"kid": "kid-1"}}
	key, err := keyFunc.Keyfunc(token)
	if err != nil {
		t.Fatalf("keyfunc: %v", err)
	}
	if key != "public-key" {
		t.Fatalf("key = %v", key)
	}
	if name, _ := token.Header["provider_name"].(string); name != "turna" {
		t.Fatalf("provider_name = %q", name)
	}

	// unknown kid falls through with ErrKIDNotFound
	token = &jwt.Token{Header: map[string]any{"kid": "other"}}
	if _, err := keyFunc.Keyfunc(token); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound, got %v", err)
	}

	// unknown issuer name also falls through
	missing := &issuerKeyFunc{providers: map[string]string{"x": "missing"}}
	if _, err := missing.Keyfunc(&jwt.Token{Header: map[string]any{}}); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound, got %v", err)
	}
}

func TestIssuerRefreshTokenData(t *testing.T) {
	IssuerRegistry.Set("refresh-auth", &fakeIssuer{kid: "kid-1", key: "public-key"})

	m := &Session{
		Provider: map[string]Provider{
			"turna": {
				AuthMiddleware: "refresh-auth",
				Oauth2:         &Oauth2{ClientID: "ui"},
			},
		},
	}

	body, err := m.refreshTokenData(context.Background(), "turna", &TokenData{RefreshToken: "r1"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if string(body) != `{"grant_type":"refresh_token"}` {
		t.Fatalf("body = %s", body)
	}

	if _, err := m.refreshTokenData(context.Background(), "unknown", &TokenData{}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
