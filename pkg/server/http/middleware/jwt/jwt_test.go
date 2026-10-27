package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/role"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/scope"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

type fixture struct {
	key  *ecdsa.PrivateKey
	jwks *httptest.Server
	pem  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	size := 32
	jwk := map[string]any{
		"kid": "k1", "kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig",
		"x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, size))),
		"y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, size))),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk}})
	}))
	t.Cleanup(srv.Close)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	return &fixture{
		key:  key,
		jwks: srv,
		pem:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
	}
}

func (f *fixture) token(t *testing.T, claims gojwt.MapClaims, kid string) string {
	t.Helper()

	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}

	tok := gojwt.NewWithClaims(gojwt.SigningMethodES256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}

	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func serve(t *testing.T, m *JWT, req *http.Request) (int, http.Header) {
	t.Helper()

	mw, err := m.Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var seen http.Header
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code, seen
}

func bearer(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req
}

func TestJWKS(t *testing.T) {
	f := newFixture(t)
	m := &JWT{
		JWKSURL:        f.jwks.URL,
		Issuer:         []string{"https://idp"},
		Audience:       []string{"api"},
		RequiredClaims: map[string]string{"scope": "read"},
		ClaimHeaders:   map[string]string{"X-User-Id": "sub", "X-Roles": "roles"},
	}

	valid := gojwt.MapClaims{"iss": "https://idp", "aud": "api", "sub": "u1", "scope": "read write", "roles": []any{"a", "b"}}

	req := bearer(f.token(t, valid, "k1"))
	req.Header.Set("X-User-Id", "forged")

	code, seen := serve(t, m, req)
	if code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	if seen.Get("X-User-Id") != "u1" || seen.Get("X-Roles") != "a,b" {
		t.Fatalf("headers %v", seen)
	}

	tests := map[string]gojwt.MapClaims{
		"issuer":   {"iss": "https://other", "aud": "api", "scope": "read"},
		"audience": {"iss": "https://idp", "aud": "other", "scope": "read"},
		"claim":    {"iss": "https://idp", "aud": "api", "scope": "write"},
		"expired":  {"iss": "https://idp", "aud": "api", "scope": "read", "exp": time.Now().Add(-time.Hour).Unix()},
	}

	for name, claims := range tests {
		if code, _ := serve(t, m, bearer(f.token(t, claims, "k1"))); code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d", name, code)
		}
	}

	if code, _ := serve(t, m, bearer(f.token(t, valid, "unknown"))); code != http.StatusUnauthorized {
		t.Errorf("unknown kid: status = %d", code)
	}
}

func TestMissingToken(t *testing.T) {
	f := newFixture(t)

	code, _ := serve(t, &JWT{PublicKeys: []string{f.pem}}, bearer(""))
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d", code)
	}

	code, _ = serve(t, &JWT{PublicKeys: []string{f.pem}, Optional: true}, bearer(""))
	if code != http.StatusNoContent {
		t.Fatalf("optional status = %d", code)
	}

	code, _ = serve(t, &JWT{PublicKeys: []string{f.pem}, Optional: true}, bearer("garbage"))
	if code != http.StatusUnauthorized {
		t.Fatalf("optional invalid status = %d", code)
	}
}

func TestStaticKeyAndSource(t *testing.T) {
	f := newFixture(t)
	m := &JWT{PublicKeys: []string{f.pem}, TokenSource: []string{"cookie:token"}, StripToken: true}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: f.token(t, gojwt.MapClaims{}, "")})
	req.AddCookie(&http.Cookie{Name: "other", Value: "1"})

	code, seen := serve(t, m, req)
	if code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	if seen.Get("Cookie") != "other=1" {
		t.Fatalf("cookie = %q", seen.Get("Cookie"))
	}
}

func TestSecret(t *testing.T) {
	m := &JWT{Secret: "s3cret"}

	tok, _ := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("s3cret"))
	if code, _ := serve(t, m, bearer(tok)); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}

	// an HS token signed with the public key must not pass a key only setup
	f := newFixture(t)
	if code, _ := serve(t, &JWT{PublicKeys: []string{f.pem}}, bearer(tok)); code != http.StatusUnauthorized {
		t.Fatalf("hs token with public key status = %d", code)
	}
}

func TestNoneRejected(t *testing.T) {
	f := newFixture(t)
	tok, _ := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)

	if code, _ := serve(t, &JWT{PublicKeys: []string{f.pem}}, bearer(tok)); code != http.StatusUnauthorized {
		t.Fatalf("status = %d", code)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*JWT{
		{},
		{Secret: "s", Algorithms: []string{"none"}},
		{JWKSURL: "http://x", Algorithms: []string{"HS256"}},
		{PublicKeys: []string{"/does/not/exist"}},
		{Secret: "s", TokenSource: []string{"body:x"}},
		{Secret: "s", Leeway: -time.Second},
	} {
		if _, err := m.Middleware(context.Background()); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}

func TestRoleAndScope(t *testing.T) {
	f := newFixture(t)

	jwtMW, err := (&JWT{PublicKeys: []string{f.pem}}).Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	roleMW, err := (&role.Role{Roles: []string{"admin"}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	scopeMW := (&scope.Scope{Scopes: []string{"api.write"}}).Middleware()

	h := jwtMW(roleMW(scopeMW(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))

	serve := func(claims gojwt.MapClaims) int {
		req := bearer(f.token(t, claims, ""))
		_, req = tcontext.New(httptest.NewRecorder(), req)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec.Code
	}

	tests := []struct {
		name   string
		claims gojwt.MapClaims
		want   int
	}{
		{"roles claim", gojwt.MapClaims{"roles": []any{"admin"}, "scope": "api.read api.write"}, http.StatusNoContent},
		{"keycloak realm roles", gojwt.MapClaims{"realm_access": map[string]any{"roles": []any{"admin"}}, "scope": "api.write"}, http.StatusNoContent},
		{"keycloak client roles", gojwt.MapClaims{"resource_access": map[string]any{"app": map[string]any{"roles": []any{"admin"}}}, "scope": "api.write"}, http.StatusNoContent},
		{"missing role", gojwt.MapClaims{"roles": []any{"user"}, "scope": "api.write"}, http.StatusForbidden},
		{"missing scope", gojwt.MapClaims{"roles": []any{"admin"}, "scope": "api.read"}, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		if code := serve(tt.claims); code != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, code, tt.want)
		}
	}
}

func TestRolesClaim(t *testing.T) {
	f := newFixture(t)

	jwtMW, err := (&JWT{
		PublicKeys: []string{f.pem},
		RolesClaim: []string{"groups", "https://example.com/roles", "app.permissions"},
	}).Middleware(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	roleMW, err := (&role.Role{Roles: []string{"admin"}}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := jwtMW(roleMW(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	tests := []struct {
		name   string
		claims gojwt.MapClaims
		want   int
	}{
		{"list", gojwt.MapClaims{"groups": []any{"dev", "admin"}}, http.StatusNoContent},
		{"namespaced", gojwt.MapClaims{"https://example.com/roles": []any{"admin"}}, http.StatusNoContent},
		{"nested", gojwt.MapClaims{"app": map[string]any{"permissions": "read admin"}}, http.StatusNoContent},
		{"comma string", gojwt.MapClaims{"groups": "dev,admin"}, http.StatusNoContent},
		{"default roles still read", gojwt.MapClaims{"roles": []any{"admin"}}, http.StatusNoContent},
		{"other claim ignored", gojwt.MapClaims{"permissions": []any{"admin"}}, http.StatusForbidden},
		{"no match", gojwt.MapClaims{"groups": []any{"dev"}}, http.StatusForbidden},
	}

	for _, tt := range tests {
		req := bearer(f.token(t, tt.claims, ""))
		_, req = tcontext.New(httptest.NewRecorder(), req)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, rec.Code, tt.want)
		}
	}
}
