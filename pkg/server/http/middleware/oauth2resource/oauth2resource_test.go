package oauth2resource

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// fakeIssuer signs and validates tokens with a test RSA key; it optionally
// keeps a revocation list.
type fakeIssuer struct {
	key     *rsa.PrivateKey
	kid     string
	revoked map[string]bool
}

func (f *fakeIssuer) Keyfunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid != f.kid {
		return nil, session.ErrKIDNotFound
	}

	return &f.key.PublicKey, nil
}

func (f *fakeIssuer) IssueToken(_ context.Context, _ url.Values) ([]byte, int, error) {
	return nil, http.StatusNotImplemented, nil
}

func (f *fakeIssuer) TokenRevoked(_ context.Context, jti string) bool {
	return f.revoked[jti]
}

func (f *fakeIssuer) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Minute).Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = f.kid

	signed, err := token.SignedString(f.key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return signed
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	return &fakeIssuer{key: key, kid: "test-kid", revoked: map[string]bool{}}
}

func newHandler(t *testing.T, m *OAuth2Resource, next http.Handler) http.Handler {
	t.Helper()

	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("middleware init: %v", err)
	}

	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}

	return middleware(next)
}

func TestMetadataURL(t *testing.T) {
	tests := []struct {
		resource string
		want     string
	}{
		{"https://example.com/mcp", "https://example.com/.well-known/oauth-protected-resource/mcp"},
		{"https://example.com", "https://example.com/.well-known/oauth-protected-resource"},
		{"https://example.com/", "https://example.com/.well-known/oauth-protected-resource"},
	}

	for _, tt := range tests {
		m := &OAuth2Resource{Resource: tt.resource}
		r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)

		if got := m.metadataURL(r); got != tt.want {
			t.Errorf("metadataURL(%q) = %q, want %q", tt.resource, got, tt.want)
		}
	}
}

func TestAudienceContains(t *testing.T) {
	if !audienceContains("https://a", "https://a") {
		t.Error("string aud should match")
	}
	if !audienceContains([]any{"turna-auth", "https://a"}, "https://a") {
		t.Error("list aud should match")
	}
	if audienceContains([]any{"turna-auth"}, "https://a") {
		t.Error("missing aud should not match")
	}
	if audienceContains(nil, "https://a") {
		t.Error("nil aud should not match")
	}
}

func TestMetadataEndpoint(t *testing.T) {
	session.IssuerRegistry.Set("res-meta", newFakeIssuer(t))

	m := &OAuth2Resource{
		AuthMiddleware:       "res-meta",
		Resource:             "https://example.com/mcp",
		AuthorizationServers: []string{"https://example.com/auth/oauth2"},
		ScopesSupported:      []string{"openid", "mcp"},
	}

	handler := newHandler(t, m, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://example.com/.well-known/oauth-protected-resource/mcp", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", rec.Code)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("metadata decode: %v", err)
	}

	if doc["resource"] != "https://example.com/mcp" {
		t.Errorf("resource = %v", doc["resource"])
	}

	servers, _ := doc["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != "https://example.com/auth/oauth2" {
		t.Errorf("authorization_servers = %v", doc["authorization_servers"])
	}
}

func TestMetadataDerived(t *testing.T) {
	session.IssuerRegistry.Set("res-derived", newFakeIssuer(t))

	m := &OAuth2Resource{AuthMiddleware: "res-derived"}
	handler := newHandler(t, m, nil)

	req := httptest.NewRequest(http.MethodGet, "http://mcp.example.com/.well-known/oauth-protected-resource", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)

	if doc["resource"] != "https://mcp.example.com" {
		t.Errorf("derived resource = %v", doc["resource"])
	}

	servers, _ := doc["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != "https://mcp.example.com/auth/oauth2" {
		t.Errorf("derived authorization_servers = %v", doc["authorization_servers"])
	}
}

func TestProtect(t *testing.T) {
	issuer := newFakeIssuer(t)
	session.IssuerRegistry.Set("res-protect", issuer)

	m := &OAuth2Resource{
		AuthMiddleware: "res-protect",
		Resource:       "https://example.com/mcp",
		RequiredScopes: []string{"mcp"},
		ScopeHeader:    "X-Scope",
	}

	var gotUser, gotScope string
	handler := newHandler(t, m, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User")
		gotScope = r.Header.Get("X-Scope")
		w.WriteHeader(http.StatusOK)
	}))

	do := func(t *testing.T, token string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest(http.MethodPost, "https://example.com/mcp", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		// spoofing attempt must be stripped
		req.Header.Set("X-User", "spoofed")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec
	}

	t.Run("missing token", func(t *testing.T) {
		rec := do(t, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}

		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(challenge, `resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp"`) {
			t.Errorf("WWW-Authenticate = %q", challenge)
		}
	})

	t.Run("garbage token", func(t *testing.T) {
		if rec := do(t, "garbage"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("refresh token rejected", func(t *testing.T) {
		token := issuer.sign(t, jwt.MapClaims{
			"typ": "Refresh", "sub": "u1", "aud": []any{"turna-auth", "https://example.com/mcp"}, "scope": "mcp",
		})
		if rec := do(t, token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		token := issuer.sign(t, jwt.MapClaims{
			"sub": "u1", "preferred_username": "user1", "aud": "turna-auth", "scope": "mcp",
		})
		if rec := do(t, token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("missing scope", func(t *testing.T) {
		token := issuer.sign(t, jwt.MapClaims{
			"sub": "u1", "preferred_username": "user1",
			"aud": []any{"turna-auth", "https://example.com/mcp"}, "scope": "openid",
		})

		rec := do(t, token)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "insufficient_scope") {
			t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
		}
	})

	t.Run("revoked token", func(t *testing.T) {
		issuer.revoked["revoked-jti"] = true
		token := issuer.sign(t, jwt.MapClaims{
			"jti": "revoked-jti", "sub": "u1", "preferred_username": "user1",
			"aud": []any{"turna-auth", "https://example.com/mcp"}, "scope": "mcp",
		})
		if rec := do(t, token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token := issuer.sign(t, jwt.MapClaims{
			"exp": time.Now().Add(-time.Minute).Unix(),
			"sub": "u1", "preferred_username": "user1",
			"aud": []any{"turna-auth", "https://example.com/mcp"}, "scope": "mcp",
		})
		if rec := do(t, token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		token := issuer.sign(t, jwt.MapClaims{
			"jti": "ok-jti", "sub": "u1", "preferred_username": "user1",
			"aud": []any{"turna-auth", "https://example.com/mcp"}, "scope": "mcp openid",
		})

		rec := do(t, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
		}
		if gotUser != "user1" {
			t.Errorf("X-User = %q, want user1", gotUser)
		}
		if gotScope != "mcp openid" {
			t.Errorf("X-Scope = %q", gotScope)
		}
	})
}

func TestMiddlewareRequiresAuthMiddleware(t *testing.T) {
	m := &OAuth2Resource{}
	if _, err := m.Middleware(); err == nil {
		t.Fatal("expected error when auth_middleware is missing")
	}
}
