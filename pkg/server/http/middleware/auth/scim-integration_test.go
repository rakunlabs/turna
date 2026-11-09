package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestSCIMIntegration provisions users and groups the way Okta and Entra ID
// do (RFC 7644).
func TestSCIMIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "integration-test-key"}}
	middleware, err := m.Middleware(ctx, "auth-scim")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	const token = "scim-integration-token-0123456789"

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	call := func(t *testing.T, method, path, body, bearer string) (int, map[string]any) {
		t.Helper()

		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, server.URL+"/auth/scim/v2"+path, reader)
		req.Header.Set("Content-Type", scimContentType)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		out := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&out)

		return res.StatusCode, out
	}

	t.Run("disabled by default", func(t *testing.T) {
		if code, _ := call(t, http.MethodGet, "/Users", "", token); code != http.StatusNotFound {
			t.Fatalf("status = %d", code)
		}
	})

	if _, err := m.store.PutSetting(ctx, "scim", json.RawMessage(`{"enabled":true,"bearer_token":"`+token+`"}`), "it"); err != nil {
		t.Fatal(err)
	}
	if err := m.cache.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("token required", func(t *testing.T) {
		if code, _ := call(t, http.MethodGet, "/Users", "", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("status = %d", code)
		}
	})

	t.Run("token is write-only", func(t *testing.T) {
		res, err := http.Get(server.URL + "/auth/v1/settings/scim")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if strings.Contains(string(body), token) {
			t.Fatalf("token leaked: %s", body)
		}
	})

	var userID, groupID string
	t.Cleanup(func() {
		bg := context.Background()
		if userID != "" {
			_ = m.store.DeleteUser(bg, userID)
		}
		if groupID != "" {
			_ = m.store.DeleteRole(bg, groupID)
		}
	})

	t.Run("create user", func(t *testing.T) {
		code, out := call(t, http.MethodPost, "/Users", `{
			"schemas":["`+scimSchemaUser+`"],"userName":"scim.jane@example.com","externalId":"okta-123",
			"name":{"givenName":"Jane","familyName":"Doe"},"emails":[{"value":"scim.jane@example.com","primary":true,"type":"work"}],
			"active":true,"password":"scim-pass-1234"}`, token)
		if code != http.StatusCreated {
			t.Fatalf("status = %d %v", code, out)
		}
		userID, _ = out["id"].(string)
		if userID == "" || out["userName"] != "scim.jane@example.com" || out["externalId"] != "okta-123" {
			t.Fatalf("user = %v", out)
		}
		if _, ok := out["password"]; ok {
			t.Fatal("password must never be returned")
		}

		if code, _ := call(t, http.MethodPost, "/Users", `{"userName":"scim.jane@example.com"}`, token); code != http.StatusConflict {
			t.Fatalf("duplicate status = %d", code)
		}
	})

	t.Run("provisioned user can sign in", func(t *testing.T) {
		raw, _ := json.Marshal(AccessClient{ClientSecret: "it-scim-secret", Scope: []string{"openid"}})
		if _, err := m.store.PutOAuthClient(ctx, "it-scim-client", raw, true, "it"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = m.store.DeleteOAuthClient(context.Background(), "it-scim-client") })
		_ = m.cache.Reload(ctx)

		form := url.Values{"grant_type": {"password"}, "username": {"scim.jane@example.com"}, "password": {"scim-pass-1234"}}
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth("it-scim-client", "it-scim-secret")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("login status = %d", res.StatusCode)
		}
	})

	t.Run("filter by userName", func(t *testing.T) {
		code, out := call(t, http.MethodGet, "/Users?filter="+url.QueryEscape(`userName eq "scim.jane@example.com"`), "", token)
		if code != http.StatusOK || out["totalResults"] != float64(1) {
			t.Fatalf("status = %d %v", code, out)
		}
		code, out = call(t, http.MethodGet, "/Users?filter="+url.QueryEscape(`userName eq "nobody@example.com"`), "", token)
		if code != http.StatusOK || out["totalResults"] != float64(0) {
			t.Fatalf("empty filter = %d %v", code, out)
		}
		if code, _ := call(t, http.MethodGet, "/Users?filter="+url.QueryEscape(`userName co "jane"`), "", token); code != http.StatusBadRequest {
			t.Fatalf("unsupported operator = %d", code)
		}
	})

	t.Run("patch user (Entra style)", func(t *testing.T) {
		code, out := call(t, http.MethodPatch, "/Users/"+userID, `{"schemas":["`+scimSchemaPatch+`"],"Operations":[
			{"op":"Replace","path":"name.givenName","value":"Janet"},
			{"op":"Replace","path":"active","value":"False"}]}`, token)
		if code != http.StatusOK {
			t.Fatalf("status = %d %v", code, out)
		}
		if out["active"] != false || out["name"].(map[string]any)["givenName"] != "Janet" {
			t.Fatalf("user = %v", out)
		}
		if !m.cache.Snapshot().Users[userID].Disabled {
			t.Fatal("user must be disabled")
		}
	})

	t.Run("patch user without path (Okta style)", func(t *testing.T) {
		code, out := call(t, http.MethodPatch, "/Users/"+userID, `{"schemas":["`+scimSchemaPatch+`"],"Operations":[
			{"op":"replace","value":{"active":true,"displayName":"Janet Doe"}}]}`, token)
		if code != http.StatusOK || out["active"] != true || out["displayName"] != "Janet Doe" {
			t.Fatalf("status = %d %v", code, out)
		}
	})

	t.Run("groups map to roles", func(t *testing.T) {
		code, out := call(t, http.MethodPost, "/Groups", `{"schemas":["`+scimSchemaGroup+`"],"displayName":"scim-engineering","members":[{"value":"`+userID+`"}]}`, token)
		if code != http.StatusCreated {
			t.Fatalf("status = %d %v", code, out)
		}
		groupID, _ = out["id"].(string)

		if user := m.cache.Snapshot().Users[userID]; !slices.Contains(user.RoleIDs, groupID) {
			t.Fatalf("membership not stored, role_ids = %v", user.RoleIDs)
		}

		code, out = call(t, http.MethodGet, "/Users/"+userID, "", token)
		groups, _ := out["groups"].([]any)
		if code != http.StatusOK || len(groups) != 1 {
			t.Fatalf("user groups = %v", out["groups"])
		}

		// remove member by filter path (Okta)
		code, _ = call(t, http.MethodPatch, "/Groups/"+groupID, `{"schemas":["`+scimSchemaPatch+`"],"Operations":[
			{"op":"remove","path":"members[value eq \"`+userID+`\"]"}]}`, token)
		if code != http.StatusOK {
			t.Fatalf("remove member = %d", code)
		}
		if user := m.cache.Snapshot().Users[userID]; slices.Contains(user.RoleIDs, groupID) {
			t.Fatal("member not removed")
		}

		// add member (Entra)
		code, _ = call(t, http.MethodPatch, "/Groups/"+groupID, `{"schemas":["`+scimSchemaPatch+`"],"Operations":[
			{"op":"Add","path":"members","value":[{"value":"`+userID+`"}]}]}`, token)
		if code != http.StatusOK || !slices.Contains(m.cache.Snapshot().Users[userID].RoleIDs, groupID) {
			t.Fatalf("add member = %d", code)
		}

		code, out = call(t, http.MethodGet, "/Groups?filter="+url.QueryEscape(`displayName eq "scim-engineering"`)+"&excludedAttributes=members", "", token)
		if code != http.StatusOK || out["totalResults"] != float64(1) {
			t.Fatalf("group filter = %d %v", code, out)
		}
	})

	t.Run("delete group clears membership", func(t *testing.T) {
		if code, _ := call(t, http.MethodDelete, "/Groups/"+groupID, "", token); code != http.StatusNoContent {
			t.Fatalf("status = %d", code)
		}
		if user := m.cache.Snapshot().Users[userID]; slices.Contains(user.RoleIDs, groupID) {
			t.Fatal("membership left behind")
		}
		groupID = ""
	})

	t.Run("delete user", func(t *testing.T) {
		if code, _ := call(t, http.MethodDelete, "/Users/"+userID, "", token); code != http.StatusNoContent {
			t.Fatalf("status = %d", code)
		}
		if code, _ := call(t, http.MethodGet, "/Users/"+userID, "", token); code != http.StatusNotFound {
			t.Fatalf("get deleted = %d", code)
		}
		userID = ""
	})

	t.Run("discovery", func(t *testing.T) {
		for _, path := range []string{"/ServiceProviderConfig", "/ResourceTypes", "/Schemas"} {
			if code, _ := call(t, http.MethodGet, path, "", token); code != http.StatusOK {
				t.Fatalf("%s = %d", path, code)
			}
		}
	})
}
