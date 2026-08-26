package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestRolesClaimPath(t *testing.T) {
	sn := &Snapshot{
		OAuthClients: map[string]AccessClient{
			"nested": {RolesClaim: "resource_access.pika.roles"},
			"plain":  {},
		},
	}

	if got := rolesClaimPath(sn, "unknown-client"); got != "roles" {
		t.Fatalf("default roles claim = %q, want %q", got, "roles")
	}

	if got := rolesClaimPath(sn, "plain"); got != "roles" {
		t.Fatalf("client without override = %q, want %q", got, "roles")
	}

	if got := rolesClaimPath(sn, "nested"); got != "resource_access.pika.roles" {
		t.Fatalf("client override = %q", got)
	}

	sn.Token = TokenSettings{RolesClaim: "realm_access.roles"}

	if got := rolesClaimPath(sn, "plain"); got != "realm_access.roles" {
		t.Fatalf("global roles claim = %q, want %q", got, "realm_access.roles")
	}

	// a per-client override still beats the global setting
	if got := rolesClaimPath(sn, "nested"); got != "resource_access.pika.roles" {
		t.Fatalf("client override under global setting = %q", got)
	}
}

// testJWTSnapshot returns the shared fixture snapshot with a usable signing
// key, so jwtRuntime resolves without a store (it only persists a generated
// key when PrivateKey is empty).
func testJWTSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	sn := testSnapshot()
	sn.JWTKey = jwtSetting{
		KID:        "test-kid",
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	}

	return sn
}

func decodeJWTPayloadForTest(t *testing.T, raw string) map[string]any {
	t.Helper()

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", raw)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	return claims
}

func claimStrings(t *testing.T, claims map[string]any, path string) []string {
	t.Helper()

	return claimValues(claims, path)
}

// TestWriteTokenPutsRolesInIDToken pins the fix for pika (and any other OIDC
// client) seeing an authenticated user with no roles: the scope-derived roles
// must land in the id_token as well as the access token, at the same dot path.
func TestWriteTokenPutsRolesInIDToken(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(testJWTSnapshot(t))

	m := &Auth{PrefixPath: "/auth", cache: cache}

	// "my-user" holds perm-1, whose Scope maps openid -> [admin].
	user, err := cache.GetUser(data.GetUserRequest{Alias: "my-user", AddScopeRoles: true})
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/auth/oauth2/token", nil)
	w := httptest.NewRecorder()

	m.writeTokenWithOptions(w, r, user, "pika", []string{"openid"}, nil, tokenIssueOptions{})

	if w.Code != http.StatusOK {
		t.Fatalf("token status = %d body = %s", w.Code, w.Body.String())
	}

	var res AccessTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	if res.IDToken == "" {
		t.Fatal("no id_token issued for the openid scope")
	}

	accessClaims := decodeJWTPayloadForTest(t, res.AccessToken)
	if got := claimStrings(t, accessClaims, "roles"); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("access token roles = %v, want [admin]", got)
	}

	idClaims := decodeJWTPayloadForTest(t, res.IDToken)
	if got := claimStrings(t, idClaims, "roles"); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("id_token roles = %v, want [admin]", got)
	}
}

// TestWriteTokenIDTokenRolesFollowClientClaimPath verifies the per-client
// roles_claim override applies to the id_token too, so a client configured for
// Keycloak-shaped claims finds them in both tokens.
func TestWriteTokenIDTokenRolesFollowClientClaimPath(t *testing.T) {
	sn := testJWTSnapshot(t)
	sn.OAuthClients = map[string]AccessClient{
		"pika": {RolesClaim: "resource_access.pika.roles"},
	}

	cache := NewCache(nil)
	cache.snap.Store(sn)

	m := &Auth{PrefixPath: "/auth", cache: cache}

	user, err := cache.GetUser(data.GetUserRequest{Alias: "my-user", AddScopeRoles: true})
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/auth/oauth2/token", nil)
	w := httptest.NewRecorder()

	m.writeTokenWithOptions(w, r, user, "pika", []string{"openid"}, nil, tokenIssueOptions{})

	if w.Code != http.StatusOK {
		t.Fatalf("token status = %d body = %s", w.Code, w.Body.String())
	}

	var res AccessTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	idClaims := decodeJWTPayloadForTest(t, res.IDToken)
	if got := claimStrings(t, idClaims, "resource_access.pika.roles"); !slices.Equal(got, []string{"admin"}) {
		t.Fatalf("id_token nested roles = %v, want [admin]", got)
	}

	if _, flat := idClaims["roles"]; flat {
		t.Fatal("id_token also carries a flat roles claim")
	}
}

// TestWriteTokenNoRolesWhenScopeGrantsNone guards the gate: roles are derived
// from the granted scopes, so a scope the user has no permission for must not
// leak roles into either token.
func TestWriteTokenNoRolesWhenScopeGrantsNone(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(testJWTSnapshot(t))

	m := &Auth{PrefixPath: "/auth", cache: cache}

	user, err := cache.GetUser(data.GetUserRequest{Alias: "my-user", AddScopeRoles: true})
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/auth/oauth2/token", nil)
	w := httptest.NewRecorder()

	// "profile" is not a key of perm-1.Scope, so it grants no roles. No
	// openid either, so no id_token is expected.
	m.writeTokenWithOptions(w, r, user, "pika", []string{"profile"}, nil, tokenIssueOptions{})

	if w.Code != http.StatusOK {
		t.Fatalf("token status = %d body = %s", w.Code, w.Body.String())
	}

	var res AccessTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	if res.IDToken != "" {
		t.Fatal("id_token issued without the openid scope")
	}

	accessClaims := decodeJWTPayloadForTest(t, res.AccessToken)
	if _, ok := accessClaims["roles"]; ok {
		t.Fatalf("access token carries roles for an unmapped scope: %v", accessClaims["roles"])
	}
}
