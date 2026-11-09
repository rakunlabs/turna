package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestJWTRotationGrace(t *testing.T) {
	cache := NewCache(nil)
	oldSnap := testJWTSnapshot(t)
	cache.snap.Store(oldSnap)
	m := &Auth{PrefixPath: "/auth", cache: cache}

	oldSigner, err := m.jwtRuntime(t.Context())
	if err != nil {
		t.Fatalf("old signer: %v", err)
	}
	oldToken, err := oldSigner.JWT.Generate(map[string]any{"aud": "turna-auth", "sub": "u1"}, time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	rotate := func(grace time.Duration, now time.Time) {
		t.Helper()

		previous, err := retainedJWTKeys(cache.Snapshot().JWTKey, grace, now)
		if err != nil {
			t.Fatalf("retain: %v", err)
		}

		next := testJWTSnapshot(t)
		next.JWTKey.KID = "kid-" + now.Format(time.RFC3339Nano)
		next.JWTKey.Previous = previous
		if err := validateJWTSetting(next.JWTKey); err != nil {
			t.Fatalf("validate rotated setting: %v", err)
		}
		cache.snap.Store(next)
	}

	rotate(time.Hour, time.Now())

	signer, err := m.jwtRuntime(t.Context())
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	if signer.KID == oldSigner.KID {
		t.Fatal("kid did not change")
	}

	if _, err := signer.Parse(oldToken, &jwt.MapClaims{}); err != nil {
		t.Fatalf("old token must verify during grace: %v", err)
	}
	if _, err := m.Keyfunc(&jwt.Token{Header: map[string]any{"kid": oldSigner.KID}}); err != nil {
		t.Fatalf("issuer keyfunc must know the previous kid: %v", err)
	}

	rec := httptest.NewRecorder()
	m.APICerts(rec, httptest.NewRequest(http.MethodGet, "/auth/oauth2/certs", nil))
	var jwks JWKSResponse
	if err := json.NewDecoder(rec.Body).Decode(&jwks); err != nil {
		t.Fatalf("decode jwks: %v", err)
	}
	if len(jwks.Keys) != 2 || jwks.Keys[0].KID != signer.KID || jwks.Keys[1].KID != oldSigner.KID {
		t.Fatalf("jwks = %+v, want active then previous key", jwks.Keys)
	}

	t.Run("retired key stops verifying", func(t *testing.T) {
		expired := *signer
		expired.Previous = []verifyKey{{KID: oldSigner.KID, Public: oldSigner.Public, RetireAt: time.Now().Add(-time.Second)}}
		if _, err := expired.Parse(oldToken, &jwt.MapClaims{}); err == nil {
			t.Fatal("retired key must not verify")
		}
	})

	t.Run("second rotation drops expired keys", func(t *testing.T) {
		previous, err := retainedJWTKeys(cache.Snapshot().JWTKey, time.Hour, time.Now().Add(2*time.Hour))
		if err != nil {
			t.Fatalf("retain: %v", err)
		}
		if len(previous) != 1 || previous[0].KID != signer.KID {
			t.Fatalf("previous = %+v, want only the replaced active key", previous)
		}
	})

	t.Run("zero grace invalidates at once", func(t *testing.T) {
		rotate(0, time.Now())

		signer, err := m.jwtRuntime(t.Context())
		if err != nil {
			t.Fatalf("signer: %v", err)
		}
		if len(signer.Previous) != 0 {
			t.Fatalf("previous = %+v, want none", signer.Previous)
		}
		if _, err := signer.Parse(oldToken, &jwt.MapClaims{}); err == nil {
			t.Fatal("old token must fail after an immediate rotation")
		}
		if _, err := m.Keyfunc(&jwt.Token{Header: map[string]any{"kid": oldSigner.KID}}); !errors.Is(err, session.ErrKIDNotFound) {
			t.Fatalf("keyfunc err = %v, want ErrKIDNotFound", err)
		}
	})
}

func TestValidateJWTSettingPrevious(t *testing.T) {
	sn := testJWTSnapshot(t)

	bad := sn.JWTKey
	bad.Previous = []jwtPreviousKey{{KID: bad.KID, PublicKey: "x"}}
	if err := validateJWTSetting(bad); err == nil {
		t.Fatal("previous kid equal to the active kid must be rejected")
	}

	bad.Previous = []jwtPreviousKey{{KID: "other", PublicKey: "not a pem"}}
	if err := validateJWTSetting(bad); err == nil {
		t.Fatal("invalid previous public key must be rejected")
	}
}
