package auth

import (
	"context"
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
)

// TestAuthMCPIntegration exercises the MCP-style OAuth stack end to end:
// RFC 8414 metadata, RFC 7591/7592 dynamic client registration, the local
// authorize + consent flow with PKCE and RFC 8707 resource binding, and
// RFC 7009 revocation with RFC 7662 introspection. Runs against a real
// PostgreSQL when AUTH_TEST_DSN is set.
func TestAuthMCPIntegration(t *testing.T) {
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

	middleware, err := m.Middleware(ctx, "auth-mcp")
	if err != nil {
		t.Fatalf("middleware init: %v", err)
	}

	if _, err := m.store.PutSetting(ctx, "registration", json.RawMessage(`{"enabled":true}`), "it"); err != nil {
		t.Fatalf("put registration setting: %v", err)
	}
	if err := m.cache.Reload(ctx); err != nil {
		t.Fatalf("cache reload: %v", err)
	}

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	// redirects are followed manually to inspect them
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// create a local user for the consent step
	var createdUser struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	{
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/v1/users",
			strings.NewReader(`{"alias":["mcp-it-user"],"local":true,"details":{"password":"mcp-it-pass","email":"mcp-it@example.com"},"is_active":true}`))
		req.Header.Set("Content-Type", "application/json")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		_ = json.NewDecoder(res.Body).Decode(&createdUser)
		res.Body.Close()

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			t.Fatalf("create user status: %d", res.StatusCode)
		}
	}
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/users/"+createdUser.Payload.ID, nil)
		res, _ := http.DefaultClient.Do(req)
		if res != nil {
			res.Body.Close()
		}
	}()

	resource := "https://mcp.example.com/mcp"
	redirectURI := "http://127.0.0.1:33418/callback"

	// --- RFC 8414 / OIDC metadata ---------------------------------------
	var metadata map[string]any
	t.Run("authorization server metadata", func(t *testing.T) {
		for _, path := range []string{
			"/.well-known/oauth-authorization-server",
			"/auth/oauth2/.well-known/oauth-authorization-server",
			"/auth/oauth2/.well-known/openid-configuration",
		} {
			res, err := http.DefaultClient.Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}

			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status: %d", path, res.StatusCode)
			}

			metadata = map[string]any{}
			_ = json.NewDecoder(res.Body).Decode(&metadata)
			res.Body.Close()

			for _, key := range []string{
				"issuer", "authorization_endpoint", "token_endpoint", "jwks_uri",
				"registration_endpoint", "revocation_endpoint", "introspection_endpoint",
			} {
				if metadata[key] == nil {
					t.Errorf("%s: metadata misses %q", path, key)
				}
			}
			if metadata["authorization_response_iss_parameter_supported"] != true || metadata["client_id_metadata_document_supported"] != true {
				t.Errorf("%s: MCP authorization metadata flags missing", path)
			}
		}
	})

	// --- RFC 7591 dynamic client registration ---------------------------
	var registration ClientRegistrationResponse
	t.Run("dynamic client registration", func(t *testing.T) {
		body := `{"redirect_uris":["` + redirectURI + `"],"token_endpoint_auth_method":"none","client_name":"MCP Test Client","grant_types":["authorization_code","refresh_token"]}`

		res, err := http.DefaultClient.Post(server.URL+"/auth/oauth2/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			t.Fatalf("register status: %d", res.StatusCode)
		}

		if err := json.NewDecoder(res.Body).Decode(&registration); err != nil {
			t.Fatalf("register decode: %v", err)
		}

		if registration.ClientID == "" || registration.ClientSecret != "" {
			t.Fatalf("public client registration wrong: %+v", registration)
		}
		if registration.RegistrationAccessToken == "" || registration.RegistrationClientURI == "" {
			t.Fatalf("registration management data missing: %+v", registration)
		}
	})

	// --- authorize + consent with PKCE ----------------------------------
	verifierRaw := make([]byte, 32)
	_, _ = rand.Read(verifierRaw)
	verifier := base64.RawURLEncoding.EncodeToString(verifierRaw)
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])

	var authCode string
	t.Run("authorize and consent", func(t *testing.T) {
		authorizeURL := server.URL + "/auth/oauth2/authorize?" + url.Values{
			"response_type":         {"code"},
			"client_id":             {registration.ClientID},
			"redirect_uri":          {redirectURI},
			"scope":                 {"openid"},
			"state":                 {"st-123"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"resource":              {resource},
		}.Encode()

		res, err := client.Get(authorizeURL)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		res.Body.Close()

		if res.StatusCode != http.StatusFound {
			t.Fatalf("authorize status: %d", res.StatusCode)
		}

		consentURL := res.Header.Get("Location")
		if !strings.Contains(consentURL, "/auth/oauth2/consent?flow=") {
			t.Fatalf("authorize redirect: %s", consentURL)
		}

		// consent without a session must not show the approval form
		res, err = client.Get(server.URL + consentURL)
		if err != nil {
			t.Fatalf("consent anonymous: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("consent anonymous status: %d", res.StatusCode)
		}

		// consent page with session
		req, _ := http.NewRequest(http.MethodGet, server.URL+consentURL, nil)
		req.Header.Set("X-User", "mcp-it-user")
		res, err = client.Do(req)
		if err != nil {
			t.Fatalf("consent: %v", err)
		}
		page := make([]byte, 16*1024)
		n, _ := res.Body.Read(page)
		res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("consent status: %d", res.StatusCode)
		}
		if !strings.Contains(string(page[:n]), "MCP Test Client") {
			t.Errorf("consent page misses client name")
		}

		flowID := consentURL[strings.Index(consentURL, "flow=")+len("flow="):]

		// approve
		form := url.Values{"flow": {flowID}, "action": {"approve"}}
		req, _ = http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/consent", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-User", "mcp-it-user")

		res, err = client.Do(req)
		if err != nil {
			t.Fatalf("consent approve: %v", err)
		}
		res.Body.Close()

		if res.StatusCode != http.StatusFound {
			t.Fatalf("consent approve status: %d", res.StatusCode)
		}

		callback, err := url.Parse(res.Header.Get("Location"))
		if err != nil {
			t.Fatalf("callback url: %v", err)
		}

		if !strings.HasPrefix(callback.String(), redirectURI) {
			t.Fatalf("callback target: %s", callback)
		}
		if callback.Query().Get("state") != "st-123" {
			t.Fatalf("callback state: %s", callback.Query().Get("state"))
		}
		if callback.Query().Get("iss") != server.URL+"/auth/oauth2" {
			t.Fatalf("callback iss: %s", callback.Query().Get("iss"))
		}

		authCode = callback.Query().Get("code")
		if authCode == "" {
			t.Fatal("callback code missing")
		}
	})

	// --- token redemption -----------------------------------------------
	var tokenResponse AccessTokenResponse
	t.Run("token with pkce and resource", func(t *testing.T) {
		// wrong client must not redeem the bound code
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"client_id":     {"someone-else"},
			"redirect_uri":  {redirectURI},
			"code_verifier": {verifier},
		}
		res, err := http.DefaultClient.PostForm(server.URL+"/auth/oauth2/token", form)
		if err != nil {
			t.Fatalf("token wrong client: %v", err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("token endpoint accepted a foreign client for a bound code")
		}

		// the code is one-time; re-run the authorize+consent quickly
		authCode = reAuthorize(t, server.URL, client, registration.ClientID, redirectURI, challenge, resource)

		form.Set("client_id", registration.ClientID)
		form.Set("code", authCode)
		form.Set("resource", resource)

		res, err = http.DefaultClient.PostForm(server.URL+"/auth/oauth2/token", form)
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("token status: %d", res.StatusCode)
		}

		if err := json.NewDecoder(res.Body).Decode(&tokenResponse); err != nil {
			t.Fatalf("token decode: %v", err)
		}

		if tokenResponse.AccessToken == "" || tokenResponse.RefreshToken == "" || tokenResponse.IDToken == "" {
			t.Fatalf("token response incomplete: %+v", tokenResponse)
		}

		// aud must contain the resource
		claims := decodeJWTClaims(t, tokenResponse.AccessToken)
		aud, _ := claims["aud"].([]any)
		found := false
		for _, a := range aud {
			if a == resource {
				found = true
			}
		}
		if !found {
			t.Errorf("access token aud misses resource: %v", claims["aud"])
		}
		if claims["jti"] == nil {
			t.Error("access token misses jti")
		}
		if claims["iss"] != server.URL+"/auth/oauth2" || claims["typ"] != "Bearer" {
			t.Errorf("access token issuer/type wrong: iss=%v typ=%v", claims["iss"], claims["typ"])
		}
		refreshClaims := decodeJWTClaims(t, tokenResponse.RefreshToken)
		if refreshClaims["iss"] != server.URL+"/auth/oauth2" || refreshClaims["typ"] != "Refresh" || refreshClaims["azp"] != registration.ClientID {
			t.Errorf("refresh token claims wrong: %v", refreshClaims)
		}
	})

	// --- introspection + revocation -------------------------------------
	t.Run("introspect and revoke", func(t *testing.T) {
		introspect := func(token string) map[string]any {
			form := url.Values{"token": {token}, "client_id": {registration.ClientID}}
			res, err := http.DefaultClient.PostForm(server.URL+"/auth/oauth2/introspect", form)
			if err != nil {
				t.Fatalf("introspect: %v", err)
			}
			defer res.Body.Close()

			out := map[string]any{}
			_ = json.NewDecoder(res.Body).Decode(&out)

			return out
		}

		if out := introspect(tokenResponse.AccessToken); out["active"] != true {
			t.Fatalf("access token should be active: %v", out)
		}

		// Refresh tokens rotate: the old token becomes unusable immediately.
		refreshForm := url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tokenResponse.RefreshToken},
			"client_id":     {registration.ClientID},
		}
		res, err := http.DefaultClient.PostForm(server.URL+"/auth/oauth2/token", refreshForm)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		var rotated AccessTokenResponse
		if err := json.NewDecoder(res.Body).Decode(&rotated); err != nil {
			res.Body.Close()
			t.Fatalf("refresh decode: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK || rotated.RefreshToken == "" || rotated.RefreshToken == tokenResponse.RefreshToken {
			t.Fatalf("refresh rotation failed: status=%d token=%+v", res.StatusCode, rotated)
		}

		res, err = http.DefaultClient.PostForm(server.URL+"/auth/oauth2/token", refreshForm)
		if err != nil {
			t.Fatalf("refresh replay: %v", err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("used refresh token was accepted")
		}

		// revoke the refresh token
		tokenResponse.RefreshToken = rotated.RefreshToken
		form := url.Values{"token": {tokenResponse.RefreshToken}, "client_id": {registration.ClientID}}
		res, err = http.DefaultClient.PostForm(server.URL+"/auth/oauth2/revoke", form)
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("revoke status: %d", res.StatusCode)
		}

		if out := introspect(tokenResponse.RefreshToken); out["active"] != false {
			t.Fatalf("revoked refresh token should be inactive: %v", out)
		}

		// a revoked refresh token must not mint new tokens
		refreshForm = url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {tokenResponse.RefreshToken},
			"client_id":     {registration.ClientID},
		}
		res, err = http.DefaultClient.PostForm(server.URL+"/auth/oauth2/token", refreshForm)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("revoked refresh token was accepted")
		}
	})

	// --- RFC 7592 client management -------------------------------------
	t.Run("registration management", func(t *testing.T) {
		managementPath := "/auth/oauth2/register/" + registration.ClientID

		// wrong token
		req, _ := http.NewRequest(http.MethodGet, server.URL+managementPath, nil)
		req.Header.Set("Authorization", "Bearer wrong")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("management get: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("management wrong token status: %d", res.StatusCode)
		}

		// read with the registration access token
		req, _ = http.NewRequest(http.MethodGet, server.URL+managementPath, nil)
		req.Header.Set("Authorization", "Bearer "+registration.RegistrationAccessToken)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("management get: %v", err)
		}
		var info ClientRegistrationResponse
		_ = json.NewDecoder(res.Body).Decode(&info)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || info.ClientID != registration.ClientID {
			t.Fatalf("management get status: %d info: %+v", res.StatusCode, info)
		}

		// delete
		req, _ = http.NewRequest(http.MethodDelete, server.URL+managementPath, nil)
		req.Header.Set("Authorization", "Bearer "+registration.RegistrationAccessToken)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("management delete: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("management delete status: %d", res.StatusCode)
		}

		// the deleted client cannot authorize anymore
		authorizeURL := server.URL + "/auth/oauth2/authorize?" + url.Values{
			"response_type": {"code"},
			"client_id":     {registration.ClientID},
			"redirect_uri":  {redirectURI},
		}.Encode()

		res, err = client.Get(authorizeURL)
		if err != nil {
			t.Fatalf("authorize deleted client: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("authorize deleted client status: %d", res.StatusCode)
		}
	})
}

// reAuthorize runs the authorize+consent flow and returns a fresh code.
func reAuthorize(t *testing.T, baseURL string, client *http.Client, clientID, redirectURI, challenge, resource string) string {
	t.Helper()

	authorizeURL := baseURL + "/auth/oauth2/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {"openid"},
		"state":                 {"st-retry"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {resource},
	}.Encode()

	res, err := client.Get(authorizeURL)
	if err != nil {
		t.Fatalf("re-authorize: %v", err)
	}
	res.Body.Close()

	consentURL := res.Header.Get("Location")
	flowID := consentURL[strings.Index(consentURL, "flow=")+len("flow="):]

	form := url.Values{"flow": {flowID}, "action": {"approve"}}
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/auth/oauth2/consent", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User", "mcp-it-user")

	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("re-consent: %v", err)
	}
	res.Body.Close()

	callback, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatalf("re-callback: %v", err)
	}

	return callback.Query().Get("code")
}

// decodeJWTClaims decodes the payload of a JWT without verification.
func decodeJWTClaims(t *testing.T, token string) map[string]any {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a jwt: %s", token)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("jwt payload decode: %v", err)
	}

	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("jwt claims decode: %v", err)
	}

	return claims
}
