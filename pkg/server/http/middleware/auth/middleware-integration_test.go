package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestMiddlewareIntegration runs the full middleware against a real PostgreSQL when AUTH_TEST_DSN is set.
func TestMiddlewareIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{
		PrefixPath: "/auth",
		Database: Database{
			DSN: dsn,
			// exercise the dedicated migration connection path
			Migration: Migration{DSN: dsn},
		},
		Encryption: Encryption{Key: "integration-test-key"},
	}

	middleware, err := m.Middleware(ctx, "auth")
	if err != nil {
		t.Fatalf("middleware init: %v", err)
	}

	// check settings now live in the database
	if _, err := m.store.PutSetting(ctx, "check", json.RawMessage(`{"no_host_check":true}`), "integration"); err != nil {
		t.Fatalf("put check setting: %v", err)
	}
	if err := m.cache.Reload(ctx); err != nil {
		t.Fatalf("cache reload: %v", err)
	}

	handler := middleware(http.NotFoundHandler())
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()

	getJSON := func(t *testing.T, path string, into any) *http.Response {
		t.Helper()

		res, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()

		if into != nil {
			if err := json.NewDecoder(res.Body).Decode(into); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
		}

		return res
	}

	// info
	var info struct {
		Payload struct {
			Storage string `json:"storage"`
		} `json:"payload"`
	}
	if res := getJSON(t, "/auth/v1/info", &info); res.StatusCode != http.StatusOK {
		t.Fatalf("info status = %d", res.StatusCode)
	}
	if info.Payload.Storage != "postgres" {
		t.Fatalf("info storage = %s", info.Payload.Storage)
	}

	// create service account
	saBody := `{"alias":["it-svc"],"details":{"secret":"it-secret","scope":"openid"},"is_active":true}`
	res, err := client.Post(server.URL+"/auth/v1/service-accounts", "application/json", strings.NewReader(saBody))
	if err != nil {
		t.Fatalf("create sa: %v", err)
	}

	var created struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode create sa: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || created.Payload.ID == "" {
		t.Fatalf("create sa status=%d id=%q", res.StatusCode, created.Payload.ID)
	}

	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/auth/v1/service-accounts/"+created.Payload.ID, nil)
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	}()

	// token via client_credentials
	form := url.Values{"grant_type": {"client_credentials"}}
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("it-svc", "it-secret")

	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}

	var tokenRes AccessTokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tokenRes); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || tokenRes.AccessToken == "" {
		t.Fatalf("token status=%d token=%q", res.StatusCode, tokenRes.AccessToken)
	}

	// wrong secret must fail
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("it-svc", "wrong")
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong secret, got %d", res.StatusCode)
	}

	// jwks
	var jwks JWKSResponse
	if res := getJSON(t, "/auth/oauth2/certs", &jwks); res.StatusCode != http.StatusOK || len(jwks.Keys) != 1 {
		t.Fatalf("jwks status=%d keys=%d", res.StatusCode, len(jwks.Keys))
	}

	// userinfo with bearer
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/auth/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenRes.AccessToken)
	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}

	var userinfo map[string]any
	if err := json.NewDecoder(res.Body).Decode(&userinfo); err != nil {
		t.Fatalf("decode userinfo: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || userinfo["sub"] != created.Payload.ID {
		t.Fatalf("userinfo status=%d sub=%v want %s", res.StatusCode, userinfo["sub"], created.Payload.ID)
	}

	// check endpoint denies unknown path
	res, err = client.Post(server.URL+"/auth/v1/check", "application/json",
		strings.NewReader(`{"alias":"it-svc","path":"/nope","method":"GET"}`))
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	var checkRes struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.NewDecoder(res.Body).Decode(&checkRes); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	res.Body.Close()
	if checkRes.Allowed {
		t.Fatal("expected check to deny")
	}

	// well-known
	var wellKnown map[string]any
	if res := getJSON(t, "/auth/oauth2/.well-known/openid-configuration", &wellKnown); res.StatusCode != http.StatusOK {
		t.Fatalf("well-known status = %d", res.StatusCode)
	}
	if wellKnown["token_endpoint"] == "" {
		t.Fatal("well-known token_endpoint missing")
	}

	// ui serves embedded index
	res, err = client.Get(server.URL + "/auth/ui/")
	if err != nil {
		t.Fatalf("ui: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ui status = %d", res.StatusCode)
	}
}
