package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/access"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
)

// TestAuthMethodsIntegration exercises the api key, device code, token
// exchange, totp, email code and mtls flows against a real PostgreSQL
// when AUTH_TEST_DSN is set.
func TestAuthMethodsIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{
		PrefixPath: "/auth",
		Database:   Database{DSN: dsn},
		Encryption: Encryption{Key: "integration-test-key"},
	}

	middleware, err := m.Middleware(ctx, "auth-methods")
	if err != nil {
		t.Fatalf("middleware init: %v", err)
	}

	// fast device polling for tests
	if _, err := m.store.PutSetting(ctx, "device", json.RawMessage(`{"interval":1}`), "it"); err != nil {
		t.Fatalf("put device setting: %v", err)
	}
	// mtls via trusted header
	if _, err := m.store.PutSetting(ctx, "mtls", json.RawMessage(`{"enabled":true,"cert_header":"ssl-client-cert"}`), "it"); err != nil {
		t.Fatalf("put mtls setting: %v", err)
	}
	// email login enabled (delivery is not exercised here)
	if _, err := m.store.PutSetting(ctx, "email", json.RawMessage(`{"smtp":{"host":"localhost"}}`), "it"); err != nil {
		t.Fatalf("put email setting: %v", err)
	}
	if err := m.cache.Reload(ctx); err != nil {
		t.Fatalf("cache reload: %v", err)
	}

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	client := server.Client()

	postJSON := func(t *testing.T, path, body string, headers map[string]string, into any) int {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer res.Body.Close()

		if into != nil {
			_ = json.NewDecoder(res.Body).Decode(into)
		}

		return res.StatusCode
	}
	patchJSON := func(t *testing.T, path, body string, headers map[string]string, into any) int {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPatch, server.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("PATCH %s: %v", path, err)
		}
		defer res.Body.Close()

		if into != nil {
			_ = json.NewDecoder(res.Body).Decode(into)
		}

		return res.StatusCode
	}
	getJSON := func(t *testing.T, path string, headers map[string]string, into any) int {
		t.Helper()

		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()

		if into != nil {
			_ = json.NewDecoder(res.Body).Decode(into)
		}

		return res.StatusCode
	}

	tokenCall := func(t *testing.T, form url.Values, headers map[string]string) (int, AccessTokenResponse, AccessTokenErrorResponse) {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("token request: %v", err)
		}
		defer res.Body.Close()

		var buf json.RawMessage
		_ = json.NewDecoder(res.Body).Decode(&buf)

		var ok AccessTokenResponse
		var fail AccessTokenErrorResponse
		_ = json.Unmarshal(buf, &ok)
		_ = json.Unmarshal(buf, &fail)

		return res.StatusCode, ok, fail
	}

	// create a confidential client (service account fallback)
	var createdClient struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if code := postJSON(t, "/auth/v1/service-accounts",
		`{"alias":["it-m-client"],"details":{"secret":"it-m-secret","scope":"openid"},"is_active":true}`,
		nil, &createdClient); code != http.StatusOK {
		t.Fatalf("create client status=%d", code)
	}

	// create a local user with password
	var createdUser struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if code := postJSON(t, "/auth/v1/users",
		`{"alias":["it-m-user"],"local":true,"details":{"password":"it-m-pass","email":"it-m-user@example.com"},"is_active":true}`,
		nil, &createdUser); code != http.StatusOK {
		t.Fatalf("create user status=%d", code)
	}

	defer func() {
		for _, p := range []string{
			"/auth/v1/service-accounts/" + createdClient.Payload.ID,
			"/auth/v1/users/" + createdUser.Payload.ID,
		} {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+p, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}
	}()

	userHeader := map[string]string{"X-User": "it-m-user"}

	// ////////////////////////////////////////////////////////////////
	t.Run("admin capability", func(t *testing.T) {
		permissionName := "it-admin-perm-" + createdUser.Payload.ID
		var createdPermission struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/permissions", `{"name":"`+permissionName+`"}`, nil, &createdPermission); code != http.StatusOK {
			t.Fatalf("create admin permission status=%d", code)
		}
		defer func() {
			_, _ = m.store.PutSetting(ctx, "admin", json.RawMessage(`{}`), "it")
			_ = m.cache.Reload(ctx)

			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/permissions/"+createdPermission.Payload.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		if _, err := m.store.PutSetting(ctx, "admin", json.RawMessage(`{"permission":"`+permissionName+`","allow_missing_x_user":true}`), "it"); err != nil {
			t.Fatalf("put admin setting: %v", err)
		}
		if err := m.cache.Reload(ctx); err != nil {
			t.Fatalf("reload admin setting: %v", err)
		}

		var deniedCaps Response[CapabilitiesResponse]
		if code := getJSON(t, "/auth/v1/capabilities", userHeader, &deniedCaps); code != http.StatusOK {
			t.Fatalf("capabilities denied status=%d", code)
		}
		if deniedCaps.Payload.IsAdmin {
			t.Fatal("user without admin permission reported admin")
		}
		if code := getJSON(t, "/auth/v1/dashboard", userHeader, nil); code != http.StatusForbidden {
			t.Fatalf("dashboard without admin permission status=%d", code)
		}

		var anonCaps Response[CapabilitiesResponse]
		if code := getJSON(t, "/auth/v1/capabilities", nil, &anonCaps); code != http.StatusOK {
			t.Fatalf("anonymous capabilities status=%d", code)
		}
		if !anonCaps.Payload.IsAdmin || !anonCaps.Payload.AnonymousAdmin {
			t.Fatalf("anonymous break-glass caps = %+v", anonCaps.Payload)
		}
		if code := getJSON(t, "/auth/v1/dashboard", nil, nil); code != http.StatusOK {
			t.Fatalf("anonymous break-glass dashboard status=%d", code)
		}

		if code := patchJSON(t, "/auth/v1/users/"+createdUser.Payload.ID,
			`{"permission_ids":["`+createdPermission.Payload.ID+`"]}`,
			nil, nil); code != http.StatusOK {
			t.Fatalf("assign admin permission status=%d", code)
		}

		var allowedCaps Response[CapabilitiesResponse]
		if code := getJSON(t, "/auth/v1/capabilities", userHeader, &allowedCaps); code != http.StatusOK {
			t.Fatalf("capabilities allowed status=%d", code)
		}
		if !allowedCaps.Payload.IsAdmin {
			t.Fatal("user with admin permission not reported admin")
		}
		if code := getJSON(t, "/auth/v1/dashboard", userHeader, nil); code != http.StatusOK {
			t.Fatalf("dashboard with admin permission status=%d", code)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("api key", func(t *testing.T) {
		permissionName := "it-api-key-perm-" + createdUser.Payload.ID
		var createdPermission struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/permissions", `{"name":"`+permissionName+`","resources":[{"paths":["/machine/*"],"methods":["GET"]}]}`, nil, &createdPermission); code != http.StatusOK {
			t.Fatalf("create api key permission status=%d", code)
		}
		defer func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/permissions/"+createdPermission.Payload.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		roleName := "it-api-key-role-" + createdUser.Payload.ID
		var createdRole struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/roles", `{"name":"`+roleName+`","permission_ids":["`+createdPermission.Payload.ID+`"]}`, nil, &createdRole); code != http.StatusOK {
			t.Fatalf("create api key role status=%d", code)
		}
		defer func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/roles/"+createdRole.Payload.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		if code := patchJSON(t, "/auth/v1/users/"+createdUser.Payload.ID,
			`{"role_ids":["`+createdRole.Payload.ID+`"]}`,
			nil, nil); code != http.StatusOK {
			t.Fatalf("assign api key role status=%d", code)
		}

		var created struct {
			Payload APIKeyCreateResponse `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/api-keys", `{"name":"it-key","role_ids":["`+createdRole.Payload.ID+`"],"permission_ids":[]}`, userHeader, &created); code != http.StatusOK {
			t.Fatalf("create api key status=%d", code)
		}
		if !strings.HasPrefix(created.Payload.Key, APIKeyPrefix) {
			t.Fatalf("api key shape invalid: %q", created.Payload.Key)
		}

		// validate the static key directly; no token exchange happens
		validateKey := func(t *testing.T, key string) (int, map[string]any) {
			t.Helper()

			req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/api-key", nil)
			req.Header.Set("X-API-Key", key)

			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("api key validate: %v", err)
			}
			defer res.Body.Close()

			claims := map[string]any{}
			_ = json.NewDecoder(res.Body).Decode(&claims)

			return res.StatusCode, claims
		}

		status, claims := validateKey(t, created.Payload.Key)
		if status != http.StatusOK {
			t.Fatalf("api key validate status=%d", status)
		}
		if claims["principal_type"] != "api_key" || claims["api_key_id"] != created.Payload.ID {
			t.Fatalf("api key claims missing: %#v", claims)
		}
		if claims["sub"] != apiKeyPrincipalSubject(created.Payload.ID) {
			t.Fatalf("api key subject = %v", claims["sub"])
		}
		if !claimStringSliceContains(claims["roles"], createdRole.Payload.ID) || !claimStringSliceContains(claims["roles"], roleName) {
			t.Fatalf("api key roles claim = %#v", claims["roles"])
		}
		if !claimStringSliceContains(claims["permissions"], createdPermission.Payload.ID) || !claimStringSliceContains(claims["permissions"], permissionName) {
			t.Fatalf("api key permissions claim = %#v", claims["permissions"])
		}

		// in-process issuer validation used by the session middleware
		if _, err := m.APIKeyData(ctx, created.Payload.Key); err != nil {
			t.Fatalf("api key issuer validation: %v", err)
		}

		var checkResp struct {
			Allowed bool `json:"allowed"`
		}
		if code := postJSON(t, "/auth/v1/check", `{"alias":"`+apiKeyPrincipalSubject(created.Payload.ID)+`","path":"/machine/job","method":"GET"}`, nil, &checkResp); code != http.StatusOK {
			t.Fatalf("api key check status=%d", code)
		}
		if !checkResp.Allowed {
			t.Fatalf("api key check denied")
		}

		// api keys are not a token grant anymore
		status, _, fail := tokenCall(t, url.Values{
			"grant_type": {"api_key"},
			"api_key":    {created.Payload.Key},
			"client_id":  {"it-m-client"},
		}, nil)
		if status != http.StatusBadRequest || fail.Error != "unsupported_grant_type" {
			t.Fatalf("api key grant should be unsupported, got status=%d error=%s", status, fail.Error)
		}

		// wrong key fails
		if status, _ := validateKey(t, APIKeyPrefix+"deadbeef"); status != http.StatusUnauthorized {
			t.Fatalf("bad api key status=%d", status)
		}

		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/api-keys/"+created.Payload.ID, nil)
		req.Header.Set("X-User", "it-m-user")
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("delete api key: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("delete api key status=%d", res.StatusCode)
		}

		// deleted key fails immediately, both over HTTP and in-process
		if status, _ := validateKey(t, created.Payload.Key); status != http.StatusUnauthorized {
			t.Fatalf("deleted api key validate status=%d", status)
		}
		if _, err := m.APIKeyData(ctx, created.Payload.Key); err == nil {
			t.Fatalf("deleted api key passed issuer validation")
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("device code", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/device_authorization",
			strings.NewReader(url.Values{"client_id": {"it-m-client"}, "client_secret": {"it-m-secret"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("device authorization: %v", err)
		}

		var device DeviceAuthorizationResponse
		_ = json.NewDecoder(res.Body).Decode(&device)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || device.DeviceCode == "" || device.UserCode == "" {
			t.Fatalf("device authorization status=%d", res.StatusCode)
		}

		form := url.Values{
			"grant_type":    {grantTypeDeviceCode},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"device_code":   {device.DeviceCode},
		}

		// pending before approval
		status, _, fail := tokenCall(t, form, nil)
		if status != http.StatusBadRequest || fail.Error != "authorization_pending" {
			t.Fatalf("expected authorization_pending, got status=%d error=%s", status, fail.Error)
		}

		// approve as user
		if code := postJSON(t, "/auth/v1/device", `{"user_code":"`+device.UserCode+`"}`, userHeader, nil); code != http.StatusOK {
			t.Fatalf("device approve status=%d", code)
		}

		time.Sleep(1100 * time.Millisecond) // respect poll interval

		status, tokenRes, fail := tokenCall(t, form, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("device grant status=%d error=%s", status, fail.Error)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("password grant required fields", func(t *testing.T) {
		base := url.Values{
			"grant_type":    {"password"},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
		}

		for name, form := range map[string]url.Values{
			"missing username": base,
			"blank username": {
				"grant_type":    {"password"},
				"client_id":     {"it-m-client"},
				"client_secret": {"it-m-secret"},
				"username":      {"   "},
				"password":      {"it-m-pass"},
			},
			"missing password": {
				"grant_type":    {"password"},
				"client_id":     {"it-m-client"},
				"client_secret": {"it-m-secret"},
				"username":      {"it-m-user"},
			},
		} {
			t.Run(name, func(t *testing.T) {
				status, _, fail := tokenCall(t, form, nil)
				if status != http.StatusBadRequest || fail.Error != "invalid_request" {
					t.Fatalf("status=%d error=%s desc=%s", status, fail.Error, fail.ErrorDescription)
				}
			})
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("signup and password reset", func(t *testing.T) {
		deleteSignupUser := func(alias string) {
			user, err := m.cache.GetUser(data.GetUserRequest{Alias: alias})
			if err != nil {
				return
			}

			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/users/"+user.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}
		defer func() {
			deleteSignupUser("it-su1@example.com")
			deleteSignupUser("it-su2@example.com")

			_, _ = m.store.PutSetting(ctx, "signup", json.RawMessage(`{}`), "it")
			_ = m.cache.Reload(ctx)
		}()

		// disabled by default
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su1@example.com","password":"signup-pass-1"}`,
			nil, nil); code != http.StatusForbidden {
			t.Fatalf("signup while disabled status=%d", code)
		}

		// enable signup without verification + password reset
		if _, err := m.store.PutSetting(ctx, "signup",
			json.RawMessage(`{"enabled":true,"email_verification":false,"password_reset":true}`), "it"); err != nil {
			t.Fatalf("put signup setting: %v", err)
		}
		if err := m.cache.Reload(ctx); err != nil {
			t.Fatalf("reload signup setting: %v", err)
		}

		// invalid client must be rejected
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"wrong","email":"it-su1@example.com","password":"signup-pass-1"}`,
			nil, nil); code != http.StatusUnauthorized {
			t.Fatalf("signup with bad client status=%d", code)
		}

		// short password must be rejected
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su1@example.com","password":"short"}`,
			nil, nil); code != http.StatusBadRequest {
			t.Fatalf("signup with short password status=%d", code)
		}

		// without verification the account is created immediately
		var created struct {
			Payload struct {
				VerificationRequired bool `json:"verification_required"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su1@example.com","name":"SU One","password":"signup-pass-1"}`,
			nil, &created); code != http.StatusOK {
			t.Fatalf("signup status=%d", code)
		}
		if created.Payload.VerificationRequired {
			t.Fatal("verification_required should be false")
		}

		// duplicate address answers 409 without verification
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su1@example.com","password":"signup-pass-1"}`,
			nil, nil); code != http.StatusConflict {
			t.Fatalf("duplicate signup status=%d", code)
		}

		// the new account can sign in
		if status, _, fail := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-su1@example.com"}, "password": {"signup-pass-1"},
		}, nil); status != http.StatusOK {
			t.Fatalf("signup login status=%d error=%s", status, fail.Error)
		}

		// switch to verification mode
		if _, err := m.store.PutSetting(ctx, "signup",
			json.RawMessage(`{"enabled":true,"email_verification":true,"password_reset":true}`), "it"); err != nil {
			t.Fatalf("put signup setting: %v", err)
		}
		if err := m.cache.Reload(ctx); err != nil {
			t.Fatalf("reload signup setting: %v", err)
		}

		// generic accepted response; account does not exist yet
		var pending struct {
			Payload struct {
				VerificationRequired bool `json:"verification_required"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/oauth2/signup",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su2@example.com","name":"SU Two","password":"signup-pass-2"}`,
			nil, &pending); code != http.StatusOK {
			t.Fatalf("signup with verification status=%d", code)
		}
		if !pending.Payload.VerificationRequired {
			t.Fatal("verification_required should be true")
		}
		if _, err := m.cache.GetUser(data.GetUserRequest{Alias: "it-su2@example.com"}); err == nil {
			t.Fatal("user must not exist before verification")
		}

		// the mailed code is unknown (only its hash is stored); simulate one
		passwordHash, err := access.ToBcrypt([]byte("signup-pass-2"))
		if err != nil {
			t.Fatalf("hash signup password: %v", err)
		}
		if err := m.store.CreateFlowCode(ctx, flowKindSignup, hashEmailCode("it-signup-code"),
			signupFlow{Email: "it-su2@example.com", Name: "SU Two", PasswordHash: passwordHash, ClientID: "it-m-client"},
			time.Minute); err != nil {
			t.Fatalf("create signup code: %v", err)
		}

		if code := postJSON(t, "/auth/oauth2/signup/verify", `{"code":"it-signup-code"}`, nil, nil); code != http.StatusOK {
			t.Fatalf("signup verify status=%d", code)
		}

		// the code is single use
		if code := postJSON(t, "/auth/oauth2/signup/verify", `{"code":"it-signup-code"}`, nil, nil); code != http.StatusUnauthorized {
			t.Fatalf("signup verify reuse status=%d", code)
		}

		// the verified account can sign in with the original password
		// (stored hash must not be re-hashed on user creation)
		if status, _, fail := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-su2@example.com"}, "password": {"signup-pass-2"},
		}, nil); status != http.StatusOK {
			t.Fatalf("verified signup login status=%d error=%s", status, fail.Error)
		}

		// password reset request always answers 200 (anti-enumeration)
		if code := postJSON(t, "/auth/oauth2/password-reset",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"it-su2@example.com"}`,
			nil, nil); code != http.StatusOK {
			t.Fatalf("password reset request status=%d", code)
		}
		if code := postJSON(t, "/auth/oauth2/password-reset",
			`{"client_id":"it-m-client","client_secret":"it-m-secret","email":"unknown@example.com"}`,
			nil, nil); code != http.StatusOK {
			t.Fatalf("password reset unknown address status=%d", code)
		}

		// confirm with a simulated reset code
		su2, err := m.cache.GetUser(data.GetUserRequest{Alias: "it-su2@example.com"})
		if err != nil {
			t.Fatalf("get signup user: %v", err)
		}
		if err := m.store.CreateFlowCode(ctx, flowKindPasswordReset, hashEmailCode("it-reset-code"),
			resetFlow{UserID: su2.ID, Email: "it-su2@example.com", ClientID: "it-m-client"},
			time.Minute); err != nil {
			t.Fatalf("create reset code: %v", err)
		}

		if code := postJSON(t, "/auth/oauth2/password-reset/confirm",
			`{"code":"it-reset-code","password":"reset-pass-9"}`, nil, nil); code != http.StatusOK {
			t.Fatalf("password reset confirm status=%d", code)
		}

		// old password fails, new password works
		if status, _, _ := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-su2@example.com"}, "password": {"signup-pass-2"},
		}, nil); status != http.StatusUnauthorized {
			t.Fatalf("old password after reset status=%d", status)
		}
		if status, _, fail := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-su2@example.com"}, "password": {"reset-pass-9"},
		}, nil); status != http.StatusOK {
			t.Fatalf("new password after reset status=%d error=%s", status, fail.Error)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("token exchange", func(t *testing.T) {
		status, subject, _ := tokenCall(t, url.Values{
			"grant_type":    {"password"},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"username":      {"it-m-user"},
			"password":      {"it-m-pass"},
		}, nil)
		if status != http.StatusOK {
			t.Fatalf("password grant status=%d", status)
		}

		status, exchanged, fail := tokenCall(t, url.Values{
			"grant_type":    {grantTypeTokenExchange},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"subject_token": {subject.AccessToken},
		}, nil)
		if status != http.StatusOK || exchanged.AccessToken == "" {
			t.Fatalf("token exchange status=%d error=%s", status, fail.Error)
		}
		if exchanged.IssuedTokenType != tokenTypeAccessToken {
			t.Fatalf("issued_token_type = %q", exchanged.IssuedTokenType)
		}

		// refresh token must not be exchangeable
		status, _, fail = tokenCall(t, url.Values{
			"grant_type":    {grantTypeTokenExchange},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"subject_token": {subject.RefreshToken},
		}, nil)
		if status != http.StatusBadRequest || fail.Error != "invalid_grant" {
			t.Fatalf("refresh exchange status=%d error=%s", status, fail.Error)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("totp", func(t *testing.T) {
		var reg struct {
			Payload TOTPRegisterResponse `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/totp/register", `{}`, userHeader, &reg); code != http.StatusOK {
			t.Fatalf("totp register status=%d", code)
		}
		if reg.Payload.Secret == "" || !strings.HasPrefix(reg.Payload.URL, "otpauth://totp/") {
			t.Fatalf("totp register payload invalid: %+v", reg.Payload)
		}

		code, err := totpCode(reg.Payload.Secret, uint64(time.Now().Unix()/totpPeriod))
		if err != nil {
			t.Fatalf("totp code: %v", err)
		}

		if status := postJSON(t, "/auth/v1/totp/confirm", `{"code":"`+code+`"}`, userHeader, nil); status != http.StatusOK {
			t.Fatalf("totp confirm status=%d", status)
		}

		passwordForm := url.Values{
			"grant_type":    {"password"},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"username":      {"it-m-user"},
			"password":      {"it-m-pass"},
		}

		// without code: mfa_required
		status, _, fail := tokenCall(t, passwordForm, nil)
		if status != http.StatusUnauthorized || fail.Error != "mfa_required" {
			t.Fatalf("expected mfa_required, got status=%d error=%s", status, fail.Error)
		}

		// with code: ok
		code, _ = totpCode(reg.Payload.Secret, uint64(time.Now().Unix()/totpPeriod))
		passwordForm.Set("totp", code)
		status, tokenRes, fail := tokenCall(t, passwordForm, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("password+totp status=%d error=%s", status, fail.Error)
		}

		// disable again for following subtests
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/totp", nil)
		req.Header.Set("X-User", "it-m-user")
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("email code", func(t *testing.T) {
		// inject a login code directly; smtp delivery is out of scope here
		if err := m.store.CreateFlowCode(ctx, flowKindEmail, hashEmailCode("it-email-code"),
			emailFlow{Alias: "it-m-user", ClientID: "it-m-client"}, time.Minute); err != nil {
			t.Fatalf("create email code: %v", err)
		}

		status, tokenRes, fail := tokenCall(t, url.Values{
			"grant_type":    {grantTypeEmailCode},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"code":          {"it-email-code"},
		}, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("email code grant status=%d error=%s", status, fail.Error)
		}

		// single use
		status, _, fail = tokenCall(t, url.Values{
			"grant_type":    {grantTypeEmailCode},
			"client_id":     {"it-m-client"},
			"client_secret": {"it-m-secret"},
			"code":          {"it-email-code"},
		}, nil)
		if status != http.StatusUnauthorized || fail.Error != "invalid_grant" {
			t.Fatalf("email code reuse status=%d error=%s", status, fail.Error)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("me", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/auth/v1/me", nil)
		req.Header.Set("X-User", "it-m-user")

		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("me: %v", err)
		}

		var me struct {
			Payload MeResponse `json:"payload"`
		}
		_ = json.NewDecoder(res.Body).Decode(&me)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !me.Payload.Local || me.Payload.ID == "" {
			t.Fatalf("me status=%d payload=%+v", res.StatusCode, me.Payload)
		}

		// password must not leak through the sanitized details
		if me.Payload.Details["password"] != nil {
			t.Fatal("me leaked password detail")
		}

		// wrong current password fails
		if code := postJSON(t, "/auth/v1/me/password",
			`{"current_password":"wrong","new_password":"new-pass-123"}`, userHeader, nil); code != http.StatusUnauthorized {
			t.Fatalf("wrong current password status=%d", code)
		}

		// change password
		if code := postJSON(t, "/auth/v1/me/password",
			`{"current_password":"it-m-pass","new_password":"new-pass-123"}`, userHeader, nil); code != http.StatusOK {
			t.Fatalf("password change status=%d", code)
		}

		// old password no longer works, new one does
		status, _, _ := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-m-user"}, "password": {"it-m-pass"},
		}, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("old password status=%d", status)
		}

		status, tokenRes, fail := tokenCall(t, url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-m-user"}, "password": {"new-pass-123"},
		}, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("new password status=%d error=%s", status, fail.Error)
		}

		// restore for other subtests
		if code := postJSON(t, "/auth/v1/me/password",
			`{"current_password":"new-pass-123","new_password":"it-m-pass"}`, userHeader, nil); code != http.StatusOK {
			t.Fatalf("password restore status=%d", code)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("totp recovery", func(t *testing.T) {
		var reg struct {
			Payload TOTPRegisterResponse `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/totp/register", `{}`, userHeader, &reg); code != http.StatusOK {
			t.Fatalf("totp register status=%d", code)
		}

		code, _ := totpCode(reg.Payload.Secret, uint64(time.Now().Unix()/totpPeriod))

		var confirm struct {
			Payload struct {
				RecoveryCodes []string `json:"recovery_codes"`
			} `json:"payload"`
		}
		if status := postJSON(t, "/auth/v1/totp/confirm", `{"code":"`+code+`"}`, userHeader, &confirm); status != http.StatusOK {
			t.Fatalf("totp confirm status=%d", status)
		}
		if len(confirm.Payload.RecoveryCodes) != totpRecoveryCodeCount {
			t.Fatalf("recovery codes count=%d", len(confirm.Payload.RecoveryCodes))
		}

		passwordForm := url.Values{
			"grant_type": {"password"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"username": {"it-m-user"}, "password": {"it-m-pass"},
			"totp": {confirm.Payload.RecoveryCodes[0]},
		}

		// recovery code works once
		status, tokenRes, fail := tokenCall(t, passwordForm, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("recovery login status=%d error=%s", status, fail.Error)
		}

		// and is consumed
		status, _, fail = tokenCall(t, passwordForm, nil)
		if status != http.StatusUnauthorized || fail.Error != "invalid_grant" {
			t.Fatalf("reused recovery code status=%d error=%s", status, fail.Error)
		}

		// cleanup totp
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/totp", nil)
		req.Header.Set("X-User", "it-m-user")
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("pkce", func(t *testing.T) {
		// inject a local code carrying an S256 challenge, like the
		// oauth2/saml callbacks do
		verifier := "it-pkce-verifier-1234567890abcdefghijklmn"
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])

		codeStore, err := m.codeStoreRuntime(ctx)
		if err != nil {
			t.Fatal(err)
		}

		makeCode := func(t *testing.T, id string) {
			t.Helper()

			codeValue, err := oauth2store.Encode(oauth2store.Code{
				Alias:               "it-m-user",
				CodeChallenge:       challenge,
				CodeChallengeMethod: "S256",
			})
			if err != nil {
				t.Fatal(err)
			}

			if err := codeStore.Code.Set(ctx, "code_"+id, codeValue); err != nil {
				t.Fatal(err)
			}
		}

		// wrong verifier rejected
		makeCode(t, "it-pkce-1")
		status, _, fail := tokenCall(t, url.Values{
			"grant_type": {"authorization_code"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"code": {"it-pkce-1"}, "code_verifier": {"wrong-verifier"},
		}, nil)
		if status != http.StatusUnauthorized || fail.Error != "invalid_grant" {
			t.Fatalf("wrong verifier status=%d error=%s", status, fail.Error)
		}

		// correct verifier passes, even without a client secret (public client)
		makeCode(t, "it-pkce-2")
		status, tokenRes, fail := tokenCall(t, url.Values{
			"grant_type": {"authorization_code"}, "client_id": {"it-m-client"}, "client_secret": {"it-m-secret"},
			"code": {"it-pkce-2"}, "code_verifier": {verifier},
		}, nil)
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("pkce exchange status=%d error=%s", status, fail.Error)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("federated sync", func(t *testing.T) {
		// role to map onto
		var createdRole struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/roles", `{"name":"it-fed-role"}`, nil, &createdRole); code != http.StatusOK {
			t.Fatalf("create role status=%d", code)
		}

		defer func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/roles/"+createdRole.Payload.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		if err := m.cache.Reload(ctx); err != nil {
			t.Fatal(err)
		}

		mapping := ClaimMapping{
			RolesClaim: "realm_access.roles",
			RoleMap:    map[string][]string{"idp-admin": {"it-fed-role"}},
			Register:   true,
		}

		claims := map[string]any{
			"email": "it-fed@example.com",
			"name":  "Fed User",
			"realm_access": map[string]any{
				"roles": []any{"idp-admin", "unmapped"},
			},
		}

		// first login: registers the user with the mapped role
		if err := m.syncFederatedUser(ctx, "it-fed@example.com", claims, mapping); err != nil {
			t.Fatalf("federated sync: %v", err)
		}

		user := m.cache.Snapshot().UserByAlias("it-fed@example.com")
		if user == nil {
			t.Fatal("federated user not created")
		}

		defer func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/users/"+user.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		if len(user.SyncRoleIDs) != 1 || user.SyncRoleIDs[0] != createdRole.Payload.ID {
			t.Fatalf("sync roles = %v, want [%s]", user.SyncRoleIDs, createdRole.Payload.ID)
		}

		// roles dropped at the IdP are dropped here too
		claims["realm_access"] = map[string]any{"roles": []any{"unmapped"}}
		if err := m.syncFederatedUser(ctx, "it-fed@example.com", claims, mapping); err != nil {
			t.Fatalf("federated re-sync: %v", err)
		}

		user = m.cache.Snapshot().UserByAlias("it-fed@example.com")
		if user == nil || len(user.SyncRoleIDs) != 0 {
			t.Fatalf("sync roles after drop = %v, want empty", user.SyncRoleIDs)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("saml", func(t *testing.T) {
		idpMetadata := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com">` +
			`<IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` +
			`<SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>` +
			`</IDPSSODescriptor></EntityDescriptor>`

		cfgRaw, _ := json.Marshal(map[string]any{"metadata_xml": idpMetadata})
		body, _ := json.Marshal(map[string]any{"config": json.RawMessage(cfgRaw)})

		req, _ := http.NewRequest(http.MethodPut, server.URL+"/auth/v1/saml/providers/it-idp", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("put saml provider: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("put saml provider status=%d", res.StatusCode)
		}

		if err := m.cache.Reload(ctx); err != nil {
			t.Fatalf("cache reload: %v", err)
		}

		// SP metadata (also generates the SP key pair on first use)
		res, err = client.Get(server.URL + "/auth/saml/it-idp/metadata")
		if err != nil {
			t.Fatalf("saml metadata: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("saml metadata status=%d", res.StatusCode)
		}

		// login must redirect to the IdP SSO endpoint
		noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}

		res, err = noRedirect.Get(server.URL + "/auth/saml/it-idp/login?redirect_uri=https://app.example.com/cb&state=xyz")
		if err != nil {
			t.Fatalf("saml login: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusTemporaryRedirect {
			t.Fatalf("saml login status=%d", res.StatusCode)
		}

		location := res.Header.Get("Location")
		if !strings.HasPrefix(location, "https://idp.example.com/sso?") ||
			!strings.Contains(location, "SAMLRequest=") || !strings.Contains(location, "RelayState=") {
			t.Fatalf("saml login redirect invalid: %s", location)
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("ui pages", func(t *testing.T) {
		// device verification page must be served via the SPA fallback
		for _, path := range []string{"/auth/ui/", "/auth/ui/device"} {
			res, err := client.Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status=%d", path, res.StatusCode)
			}
		}
	})

	// ////////////////////////////////////////////////////////////////
	t.Run("mtls", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}

		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "it-m-mtls"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		}

		der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}

		cert, _ := x509.ParseCertificate(der)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

		// service account bound to the certificate fingerprint
		var createdMTLS struct {
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if code := postJSON(t, "/auth/v1/service-accounts",
			`{"alias":["it-m-mtls"],"details":{"cert_fingerprint":"`+certFingerprint(cert)+`"},"is_active":true}`,
			nil, &createdMTLS); code != http.StatusOK {
			t.Fatalf("create mtls sa status=%d", code)
		}

		defer func() {
			req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/service-accounts/"+createdMTLS.Payload.ID, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
			}
		}()

		status, tokenRes, fail := tokenCall(t, url.Values{
			"grant_type": {"client_credentials"},
			"client_id":  {"it-m-mtls"},
		}, map[string]string{"ssl-client-cert": url.QueryEscape(string(certPEM))})
		if status != http.StatusOK || tokenRes.AccessToken == "" {
			t.Fatalf("mtls grant status=%d error=%s", status, fail.Error)
		}

		// without certificate fails
		status, _, fail = tokenCall(t, url.Values{
			"grant_type": {"client_credentials"},
			"client_id":  {"it-m-mtls"},
		}, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("mtls without cert status=%d error=%s", status, fail.Error)
		}
	})
}

func claimStringSliceContains(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}

	for _, item := range items {
		if got, _ := item.(string); got == want {
			return true
		}
	}

	return false
}
