package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRevokeRawTokenSkipsOtherClientsToken(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(testJWTSnapshot(t))
	// no store: reaching the revocation write would panic
	m := &Auth{cache: cache}

	signer, err := m.jwtRuntime(t.Context())
	if err != nil {
		t.Fatalf("jwt runtime: %v", err)
	}

	token, err := signer.JWT.Generate(map[string]any{"jti": "j1", "azp": "client-a", "typ": "Bearer"}, time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if err := m.revokeRawToken(t.Context(), token, "client-b"); err != nil {
		t.Fatalf("revoke by another client: %v", err)
	}
}

func TestTokenClientID(t *testing.T) {
	if got := tokenClientID(jwt.MapClaims{"azp": "a", "client_id": "b"}); got != "a" {
		t.Errorf("azp first, got %q", got)
	}
	if got := tokenClientID(jwt.MapClaims{"client_id": "b"}); got != "b" {
		t.Errorf("client_id fallback, got %q", got)
	}
	if got := tokenClientID(jwt.MapClaims{}); got != "" {
		t.Errorf("no client, got %q", got)
	}
}
