package jwks

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func rsaJWK(t *testing.T, kid string, pub *rsa.PublicKey, alg string) map[string]any {
	t.Helper()

	return map[string]any{
		"kid": kid,
		"kty": "RSA",
		"alg": alg,
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWK(t *testing.T, kid string, pub *ecdsa.PublicKey) map[string]any {
	t.Helper()

	size := (pub.Curve.Params().BitSize + 7) / 8

	return map[string]any{
		"kid": kid,
		"kty": "EC",
		"crv": pub.Curve.Params().Name,
		"use": "sig",
		"x":   base64.RawURLEncoding.EncodeToString(pub.X.FillBytes(make([]byte, size))),
		"y":   base64.RawURLEncoding.EncodeToString(pub.Y.FillBytes(make([]byte, size))),
	}
}

func serveJWKS(t *testing.T, keys *atomic.Value) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys.Load()})
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestGetAndKeyfunc(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var keys atomic.Value
	keys.Store([]map[string]any{
		rsaJWK(t, "rsa-1", &rsaKey.PublicKey, "RS256"),
		ecJWK(t, "ec-1", &ecKey.PublicKey),
		{
			"kid": "ed-1", "kty": "OKP", "crv": "Ed25519", "use": "sig",
			"x": base64.RawURLEncoding.EncodeToString(edPub),
		},
		{
			"kid": "oct-1", "kty": "oct",
			"k": base64.RawURLEncoding.EncodeToString([]byte("secret-hmac-key")),
		},
		{"kid": "enc-1", "kty": "RSA", "use": "enc", "n": "AQAB", "e": "AQAB"}, // filtered: use=enc
		{"kid": "bad-1", "kty": "RSA", "n": "!!!!", "e": "AQAB"},               // skipped: broken
		{"kid": "unknown-1", "kty": "XXX"},                                     // skipped: unsupported
	})

	srv := serveJWKS(t, &keys)

	j, err := Get(srv.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if j.Len() != 4 {
		t.Fatalf("Len() = %d, want 4", j.Len())
	}

	// RSA
	key, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "rsa-1", "alg": "RS256"}})
	if err != nil {
		t.Fatalf("Keyfunc rsa-1: %v", err)
	}
	if _, ok := key.(*rsa.PublicKey); !ok {
		t.Fatalf("rsa-1 key type = %T", key)
	}

	// EC
	key, err = j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "ec-1", "alg": "ES256"}})
	if err != nil {
		t.Fatalf("Keyfunc ec-1: %v", err)
	}
	if _, ok := key.(*ecdsa.PublicKey); !ok {
		t.Fatalf("ec-1 key type = %T", key)
	}

	// Ed25519
	key, err = j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "ed-1", "alg": "EdDSA"}})
	if err != nil {
		t.Fatalf("Keyfunc ed-1: %v", err)
	}
	if _, ok := key.(ed25519.PublicKey); !ok {
		t.Fatalf("ed-1 key type = %T", key)
	}

	// oct
	key, err = j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "oct-1", "alg": "HS256"}})
	if err != nil {
		t.Fatalf("Keyfunc oct-1: %v", err)
	}
	if string(key.([]byte)) != "secret-hmac-key" {
		t.Fatalf("oct-1 key = %q", key)
	}

	// unknown kid
	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "nope"}}); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound, got %v", err)
	}

	// missing kid header
	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{}}); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("expected ErrKIDNotFound for missing kid, got %v", err)
	}

	// alg mismatch
	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "rsa-1", "alg": "HS256"}}); !errors.Is(err, ErrJWKAlgMismatch) {
		t.Fatalf("expected ErrJWKAlgMismatch, got %v", err)
	}
}

func TestParseSignedJWT(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var keys atomic.Value
	keys.Store([]map[string]any{
		rsaJWK(t, "rsa-1", &rsaKey.PublicKey, "RS256"),
		ecJWK(t, "ec-1", &ecKey.PublicKey),
	})

	srv := serveJWKS(t, &keys)

	j, err := Get(srv.URL, Options{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// RS256
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "tester"})
	tok.Header["kid"] = "rsa-1"

	signed, err := tok.SignedString(rsaKey)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := jwt.ParseWithClaims(signed, jwt.MapClaims{}, j.Keyfunc)
	if err != nil {
		t.Fatalf("parse RS256: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("RS256 token not valid")
	}

	// ES256
	tok = jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{"sub": "tester"})
	tok.Header["kid"] = "ec-1"

	signed, err = tok.SignedString(ecKey)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err = jwt.ParseWithClaims(signed, jwt.MapClaims{}, j.Keyfunc)
	if err != nil {
		t.Fatalf("parse ES256: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("ES256 token not valid")
	}
}

func TestGetInitialFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if _, err := Get(srv.URL, Options{}); err == nil {
		t.Fatal("expected error on initial fetch failure")
	}
}

func TestBackgroundRefresh(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	var keys atomic.Value
	keys.Store([]map[string]any{rsaJWK(t, "old-kid", &rsaKey.PublicKey, "RS256")})

	srv := serveJWKS(t, &keys)

	j, err := Get(srv.URL, Options{RefreshInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer j.EndBackground()

	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "old-kid"}}); err != nil {
		t.Fatalf("old-kid before rotation: %v", err)
	}

	// rotate keys
	keys.Store([]map[string]any{rsaJWK(t, "new-kid", &rsaKey.PublicKey, "RS256")})

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "new-kid"}}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("new-kid never appeared after rotation")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "old-kid"}}); !errors.Is(err, ErrKIDNotFound) {
		t.Fatalf("old-kid after rotation: %v", err)
	}
}

func TestRefreshErrorHandler(t *testing.T) {
	var failing atomic.Bool

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{rsaJWK(t, "kid-1", &rsaKey.PublicKey, "RS256")}})
	}))
	t.Cleanup(srv.Close)

	errCh := make(chan error, 1)

	j, err := Get(srv.URL, Options{
		RefreshInterval: 20 * time.Millisecond,
		RefreshErrorHandler: func(err error) {
			select {
			case errCh <- err:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer j.EndBackground()

	failing.Store(true)

	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh error handler was not called")
	}

	// keys survive failed refresh
	if _, err := j.Keyfunc(&jwt.Token{Header: map[string]any{"kid": "kid-1"}}); err != nil {
		t.Fatalf("key lost after failed refresh: %v", err)
	}
}
