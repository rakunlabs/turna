package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestDPoPIntegration binds tokens to a DPoP key (RFC 9449) and checks the
// binding on userinfo and refresh.
func TestDPoPIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "integration-test-key"}}
	middleware, err := m.Middleware(ctx, "auth-dpop")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	raw, _ := json.Marshal(AccessClient{ClientSecret: "it-dpop-secret", Scope: []string{"openid"}})
	if _, err := m.store.PutOAuthClient(ctx, "it-dpop-client", raw, true, "it"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.store.DeleteOAuthClient(context.Background(), "it-dpop-client") })

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	{
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/v1/users",
			strings.NewReader(`{"alias":["it-dpop-user"],"local":true,"details":{"name":"DPoP","password":"it-dpop-pass-1234"},"is_active":true}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		_ = json.NewDecoder(res.Body).Decode(&created)
		res.Body.Close()
		t.Cleanup(func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/users/"+created.Payload.ID, nil)
			if res, _ := http.DefaultClient.Do(req); res != nil {
				res.Body.Close()
			}
		})
	}
	if err := m.cache.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	newKey := func() (*ecdsa.PrivateKey, map[string]any) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pad := func(b []byte) string {
			out := make([]byte, 32)
			copy(out[32-len(b):], b)

			return base64.RawURLEncoding.EncodeToString(out)
		}

		return key, map[string]any{"kty": "EC", "crv": "P-256", "x": pad(key.X.Bytes()), "y": pad(key.Y.Bytes())}
	}
	key, jwk := newKey()
	otherKey, otherJWK := newKey()

	proof := func(k *ecdsa.PrivateKey, j map[string]any, htm, path, accessToken string) string {
		claims := jwt.MapClaims{"jti": time.Now().Format(time.RFC3339Nano), "htm": htm, "htu": server.URL + path, "iat": time.Now().Unix()}
		if accessToken != "" {
			sum := sha256.Sum256([]byte(accessToken))
			claims["ath"] = base64.RawURLEncoding.EncodeToString(sum[:])
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		tok.Header["typ"] = "dpop+jwt"
		tok.Header["jwk"] = j
		s, err := tok.SignedString(k)
		if err != nil {
			t.Fatal(err)
		}

		return s
	}

	token := func(form url.Values, dpopProof string) (int, AccessTokenResponse) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth("it-dpop-client", "it-dpop-secret")
		if dpopProof != "" {
			req.Header.Set("DPoP", dpopProof)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		var tok AccessTokenResponse
		_ = json.NewDecoder(res.Body).Decode(&tok)

		return res.StatusCode, tok
	}

	userinfo := func(scheme, accessToken, dpopProof string) int {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/auth/oauth2/userinfo", nil)
		req.Header.Set("Authorization", scheme+" "+accessToken)
		if dpopProof != "" {
			req.Header.Set("DPoP", dpopProof)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()

		return res.StatusCode
	}

	password := url.Values{"grant_type": {"password"}, "username": {"it-dpop-user"}, "password": {"it-dpop-pass-1234"}, "scope": {"openid"}}

	status, tok := token(password, proof(key, jwk, "POST", "/auth/oauth2/token", ""))
	if status != http.StatusOK || tok.TokenType != "DPoP" {
		t.Fatalf("dpop token status %d type %q", status, tok.TokenType)
	}
	if jkt := tokenJKT(decodeJWTPayloadForTest(t, tok.AccessToken)); jkt == "" {
		t.Fatal("access token has no cnf.jkt")
	}

	t.Run("userinfo needs the bound key", func(t *testing.T) {
		if code := userinfo("DPoP", tok.AccessToken, proof(key, jwk, "GET", "/auth/oauth2/userinfo", tok.AccessToken)); code != http.StatusOK {
			t.Fatalf("bound userinfo = %d", code)
		}
		if code := userinfo("Bearer", tok.AccessToken, ""); code != http.StatusUnauthorized {
			t.Fatalf("bound token as bearer = %d", code)
		}
		if code := userinfo("DPoP", tok.AccessToken, proof(otherKey, otherJWK, "GET", "/auth/oauth2/userinfo", tok.AccessToken)); code != http.StatusUnauthorized {
			t.Fatalf("other key = %d", code)
		}
	})

	t.Run("proof replay is refused", func(t *testing.T) {
		p := proof(key, jwk, "GET", "/auth/oauth2/userinfo", tok.AccessToken)
		if code := userinfo("DPoP", tok.AccessToken, p); code != http.StatusOK {
			t.Fatalf("first use = %d", code)
		}
		if code := userinfo("DPoP", tok.AccessToken, p); code != http.StatusUnauthorized {
			t.Fatalf("replay = %d", code)
		}
	})

	t.Run("refresh keeps the binding", func(t *testing.T) {
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}
		if status, _ := token(refresh, ""); status != http.StatusBadRequest {
			t.Fatalf("refresh without proof = %d", status)
		}
		if status, _ := token(refresh, proof(otherKey, otherJWK, "POST", "/auth/oauth2/token", "")); status != http.StatusBadRequest {
			t.Fatalf("refresh with other key = %d", status)
		}
		status, refreshed := token(refresh, proof(key, jwk, "POST", "/auth/oauth2/token", ""))
		if status != http.StatusOK || refreshed.TokenType != "DPoP" {
			t.Fatalf("refresh with key = %d %q", status, refreshed.TokenType)
		}
	})

	t.Run("bearer tokens stay bearer", func(t *testing.T) {
		status, plain := token(password, "")
		if status != http.StatusOK || plain.TokenType != "Bearer" {
			t.Fatalf("bearer token = %d %q", status, plain.TokenType)
		}
		if code := userinfo("Bearer", plain.AccessToken, ""); code != http.StatusOK {
			t.Fatalf("bearer userinfo = %d", code)
		}
	})

	t.Run("invalid proof at token endpoint", func(t *testing.T) {
		if status, _ := token(password, "not-a-jwt"); status != http.StatusBadRequest {
			t.Fatalf("status = %d", status)
		}
	})
}
