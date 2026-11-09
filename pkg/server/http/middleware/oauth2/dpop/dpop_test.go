package dpop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type proofKey struct {
	key *ecdsa.PrivateKey
	jwk map[string]any
}

func newProofKey(t *testing.T) proofKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pad := func(b []byte) []byte {
		out := make([]byte, 32)
		copy(out[32-len(b):], b)

		return out
	}

	return proofKey{key: key, jwk: map[string]any{
		"kty": "EC", "crv": "P-256",
		"x": base64.RawURLEncoding.EncodeToString(pad(key.X.Bytes())),
		"y": base64.RawURLEncoding.EncodeToString(pad(key.Y.Bytes())),
	}}
}

func (k proofKey) proof(t *testing.T, htm, htu, accessToken string, mutate func(jwt.MapClaims, map[string]any)) string {
	t.Helper()

	claims := jwt.MapClaims{"jti": time.Now().String(), "htm": htm, "htu": htu, "iat": time.Now().Unix()}
	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		claims["ath"] = base64.RawURLEncoding.EncodeToString(sum[:])
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["typ"] = "dpop+jwt"
	tok.Header["jwk"] = k.jwk
	if mutate != nil {
		mutate(claims, tok.Header)
	}

	raw, err := tok.SignedString(k.key)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func request(method, target, proof string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	if proof != "" {
		r.Header.Set("DPoP", proof)
	}

	return r
}

func TestVerify(t *testing.T) {
	k := newProofKey(t)
	const htu = "https://as.example.com/auth/oauth2/token"

	proof, err := Verify(request(http.MethodPost, "/auth/oauth2/token", k.proof(t, "POST", htu, "", nil)), htu, "", nil)
	if err != nil {
		t.Fatalf("valid proof: %v", err)
	}
	if proof.JKT == "" {
		t.Fatal("missing thumbprint")
	}

	cases := map[string]func(jwt.MapClaims, map[string]any){
		"wrong typ":   func(_ jwt.MapClaims, h map[string]any) { h["typ"] = "JWT" },
		"wrong htm":   func(c jwt.MapClaims, _ map[string]any) { c["htm"] = "GET" },
		"wrong htu":   func(c jwt.MapClaims, _ map[string]any) { c["htu"] = "https://evil.example/token" },
		"old iat":     func(c jwt.MapClaims, _ map[string]any) { c["iat"] = time.Now().Add(-time.Hour).Unix() },
		"missing jti": func(c jwt.MapClaims, _ map[string]any) { delete(c, "jti") },
		"no jwk":      func(_ jwt.MapClaims, h map[string]any) { delete(h, "jwk") },
		"private jwk": func(_ jwt.MapClaims, h map[string]any) {
			h["jwk"] = map[string]any{"kty": "EC", "crv": "P-256", "x": k.jwk["x"], "y": k.jwk["y"], "d": "AA"}
		},
	}
	for name, mutate := range cases {
		if _, err := Verify(request(http.MethodPost, "/auth/oauth2/token", k.proof(t, "POST", htu, "", mutate)), htu, "", nil); err == nil {
			t.Errorf("%s: proof must be rejected", name)
		}
	}

	t.Run("query and case are ignored in htu", func(t *testing.T) {
		raw := k.proof(t, "POST", "HTTPS://AS.example.com/auth/oauth2/token", "", nil)
		if _, err := Verify(request(http.MethodPost, "/auth/oauth2/token?x=1", raw), htu+"", "", nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("missing or duplicate header", func(t *testing.T) {
		if _, err := Verify(request(http.MethodPost, "/", ""), htu, "", nil); !errors.Is(err, ErrMissing) {
			t.Fatalf("err = %v", err)
		}
		r := request(http.MethodPost, "/", k.proof(t, "POST", htu, "", nil))
		r.Header.Add("DPoP", k.proof(t, "POST", htu, "", nil))
		if _, err := Verify(r, htu, "", nil); !errors.Is(err, ErrMissing) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("replay", func(t *testing.T) {
		cache := &MemoryReplay{}
		raw := k.proof(t, "POST", htu, "", nil)
		if _, err := Verify(request(http.MethodPost, "/", raw), htu, "", cache.Seen); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(request(http.MethodPost, "/", raw), htu, "", cache.Seen); !errors.Is(err, ErrReplay) {
			t.Fatalf("replay err = %v", err)
		}
	})
}

func TestCheckBinding(t *testing.T) {
	k := newProofKey(t)
	other := newProofKey(t)
	const htu = "https://api.example.com/items"
	const token = "access-token-value"

	jkt, err := Verify(request(http.MethodGet, "/items", k.proof(t, "GET", htu, "", nil)), htu, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	bound := map[string]any{"cnf": map[string]any{"jkt": jkt.JKT}}

	if err := CheckBinding(request(http.MethodGet, "/items", k.proof(t, "GET", htu, token, nil)), bound, "DPoP", token, htu, nil); err != nil {
		t.Fatalf("bound token with proof: %v", err)
	}
	if err := CheckBinding(request(http.MethodGet, "/items", ""), bound, "Bearer", token, htu, nil); err == nil {
		t.Fatal("bound token as bearer must fail")
	}
	if err := CheckBinding(request(http.MethodGet, "/items", other.proof(t, "GET", htu, token, nil)), bound, "DPoP", token, htu, nil); err == nil {
		t.Fatal("proof of another key must fail")
	}
	if err := CheckBinding(request(http.MethodGet, "/items", k.proof(t, "GET", htu, "other-token", nil)), bound, "DPoP", token, htu, nil); err == nil {
		t.Fatal("ath for another token must fail")
	}
	if err := CheckBinding(request(http.MethodGet, "/items", ""), map[string]any{}, "Bearer", token, htu, nil); err != nil {
		t.Fatalf("plain bearer: %v", err)
	}
	if err := CheckBinding(request(http.MethodGet, "/items", ""), map[string]any{}, "DPoP", token, htu, nil); !errors.Is(err, ErrNotBound) {
		t.Fatalf("DPoP scheme for bearer token err = %v", err)
	}
}
