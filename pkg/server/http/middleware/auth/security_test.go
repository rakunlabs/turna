package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

func TestDynamicClientGrantPolicy(t *testing.T) {
	client := AccessClient{Dynamic: true, GrantTypes: []string{"authorization_code"}}
	if !client.allowsGrant("authorization_code") || client.allowsGrant("password") || client.allowsGrant("refresh_token") {
		t.Fatal("registered grants not enforced")
	}
	legacy := AccessClient{Dynamic: true}
	if legacy.allowsGrant("password") || legacy.allowsGrant(grantTypeTokenExchange) || !legacy.allowsGrant("refresh_token") {
		t.Fatal("legacy DCR must default to safe registration grants")
	}
	if !(AccessClient{}).allowsGrant("password") {
		t.Fatal("static clients without restrictions must remain compatible")
	}
}

func TestExpiredDynamicClientLookup(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Registration: RegistrationSettings{clientLifetime: time.Hour}, OAuthClients: map[string]AccessClient{
		"old": {Dynamic: true, CreatedAt: time.Now().Add(-100 * 365 * 24 * time.Hour).Unix()},
	}})
	if _, ok := (&Auth{cache: cache}).lookupClient("old"); ok {
		t.Fatal("expired client was usable")
	}
}

func TestDynamicClientCannotUsePasswordGrant(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{OAuthClients: map[string]AccessClient{"dynamic": {Dynamic: true, ClientSecret: "secret", GrantTypes: []string{"authorization_code"}}}})
	form := url.Values{"client_id": {"dynamic"}, "client_secret": {"secret"}, "grant_type": {"password"}}
	r := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	(&Auth{cache: cache}).APIToken(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unauthorized_client") {
		t.Fatalf("grant: %d %s", w.Code, w.Body.String())
	}
}

func TestOwnedKeyLosesRevokedAccess(t *testing.T) {
	m := &Auth{cache: testCache()}
	owner, err := m.cache.GetUser(data.GetUserRequest{ID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	key := &APIKeyMeta{ID: "key", UserID: owner.ID, RoleIDs: []string{"role-parent"}, PermissionIDs: []string{"perm-1"}}
	before := m.apiKeyUser(key, owner)
	if !hasAccessID(before.Permissions, "perm-1") {
		t.Fatal("initial permissions missing")
	}
	copyOwner := *owner.User
	copyOwner.RoleIDs = nil
	copyOwner.SyncRoleIDs = nil
	copyOwner.TmpRoleIDs = nil
	copyOwner.PermissionIDs = nil
	copyOwner.TmpPermissionIDs = nil
	copyExt := *owner
	copyExt.User = &copyOwner
	after := m.apiKeyUser(key, &copyExt)
	if len(after.Roles) != 0 || len(after.Permissions) != 0 {
		t.Fatalf("key retained revoked permissions: %+v", after)
	}
}

func TestNormalizeAliases(t *testing.T) {
	got := normalizedAliases([]string{"  Alice@EXAMPLE.COM ", "alice@example.com", "", "  ", "UserName"})
	if len(got) != 2 || got[0] != "alice@example.com" || got[1] != "UserName" {
		t.Fatalf("aliases=%v", got)
	}
}

func TestSecretsRedactionAndMerge(t *testing.T) {
	stored := []byte(`{"client_secret":"secret-value","bind":{"password":"bind-value"},"providers":{"removed":{"client_secret":"old"}}}`)
	redacted, err := redactSecrets(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), "secret-value") || strings.Contains(string(redacted), "bind-value") {
		t.Fatalf("redaction: %s", redacted)
	}
	merged, err := mergeSecrets([]byte(`{"client_secret":"","bind":{"password":null},"providers":{}}`), stored)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["client_secret"] != "secret-value" || doc["bind"].(map[string]any)["password"] != "" {
		t.Fatalf("merge: %s", merged)
	}
	if strings.Contains(string(merged), "removed") {
		t.Fatalf("removed provider was resurrected: %s", merged)
	}
	roles := []byte(`{"role_map":{"password":["reader"],"secret":["writer"]}}`)
	redacted, err = redactSecrets(roles)
	if err != nil || !strings.Contains(string(redacted), `"reader"`) || !strings.Contains(string(redacted), `"writer"`) {
		t.Fatalf("noncredential mapping was masked: %s %v", redacted, err)
	}
	if _, err := mergeSecrets([]byte(`null`), stored); err == nil {
		t.Fatal("null config must be rejected, not panic")
	}
}
