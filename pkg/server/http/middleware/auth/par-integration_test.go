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

// TestPARAndEndSessionIntegration covers RFC 9126 pushed authorization
// requests and OpenID Connect RP-Initiated Logout.
func TestPARAndEndSessionIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "integration-test-key"}}
	middleware, err := m.Middleware(ctx, "auth-par")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	const (
		clientID    = "it-par-client"
		secret      = "it-par-secret"
		redirectURI = "https://app.example.com/cb"
		logoutURI   = "https://app.example.com/bye"
		alias       = "it-par-user"
		challenge   = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
		verifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	)

	raw, _ := json.Marshal(AccessClient{
		ClientSecret: secret, Scope: []string{"openid"}, RedirectURIs: []string{redirectURI},
		PostLogoutRedirectURIs: []string{logoutURI}, SkipConsent: true,
	})
	if _, err := m.store.PutOAuthClient(ctx, clientID, raw, true, "it"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.store.DeleteOAuthClient(context.Background(), clientID) })

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	{
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/v1/users",
			strings.NewReader(`{"alias":["`+alias+`"],"local":true,"details":{"name":"PAR"},"is_active":true}`))
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

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	push := func(t *testing.T, form url.Values, user, pass string) (int, map[string]any) {
		t.Helper()

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/oauth2/par", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		body := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&body)

		return res.StatusCode, body
	}

	authorizeParams := url.Values{
		"response_type": {"code"}, "redirect_uri": {redirectURI}, "scope": {"openid"},
		"state": {"par-state"}, "nonce": {"n1"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}

	// follow authorize → consent → client redirect as a logged-in browser
	authorize := func(t *testing.T, query url.Values) (*http.Response, *url.URL) {
		t.Helper()

		target := server.URL + "/auth/oauth2/authorize?" + query.Encode()
		for range 4 {
			req, _ := http.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("X-User", alias)
			res, err := noFollow.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()

			loc, err := res.Location()
			if err != nil {
				return res, nil
			}
			if loc.Host != "" && !strings.HasPrefix(loc.String(), server.URL) {
				return res, loc
			}
			target = server.URL + loc.RequestURI()
		}

		t.Fatal("too many redirects")

		return nil, nil
	}

	var idToken string

	t.Run("pushed request is redeemed once", func(t *testing.T) {
		status, body := push(t, authorizeParams, clientID, secret)
		if status != http.StatusCreated {
			t.Fatalf("push status %d %v", status, body)
		}
		requestURI, _ := body["request_uri"].(string)
		if !strings.HasPrefix(requestURI, parRequestURIPrefix) || body["expires_in"] == nil {
			t.Fatalf("push response = %v", body)
		}

		_, loc := authorize(t, url.Values{"client_id": {clientID}, "request_uri": {requestURI}})
		if loc == nil || loc.Query().Get("code") == "" || loc.Query().Get("state") != "par-state" {
			t.Fatalf("authorize redirect = %v", loc)
		}

		form := url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {redirectURI},
			"client_id": {clientID}, "client_secret": {secret}, "code_verifier": {verifier}}
		res, err := http.PostForm(server.URL+"/auth/oauth2/token", form)
		if err != nil {
			t.Fatal(err)
		}
		var tok AccessTokenResponse
		_ = json.NewDecoder(res.Body).Decode(&tok)
		res.Body.Close()
		if tok.IDToken == "" {
			t.Fatalf("token status %d", res.StatusCode)
		}
		idToken = tok.IDToken

		res, _ = authorize(t, url.Values{"client_id": {clientID}, "request_uri": {requestURI}})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("replayed request_uri status = %d", res.StatusCode)
		}
	})

	t.Run("push rejects bad client and redirect", func(t *testing.T) {
		if status, _ := push(t, authorizeParams, clientID, "wrong"); status != http.StatusUnauthorized {
			t.Fatalf("wrong secret status = %d", status)
		}

		bad := url.Values{}
		for k, v := range authorizeParams {
			bad[k] = v
		}
		bad.Set("redirect_uri", "https://evil.example/cb")
		if status, _ := push(t, bad, clientID, secret); status != http.StatusBadRequest {
			t.Fatalf("bad redirect status = %d", status)
		}
	})

	t.Run("request_uri bound to the pushing client", func(t *testing.T) {
		_, body := push(t, authorizeParams, clientID, secret)
		requestURI, _ := body["request_uri"].(string)

		res, _ := authorize(t, url.Values{"client_id": {"other-client"}, "request_uri": {requestURI}})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("other client status = %d", res.StatusCode)
		}
	})

	t.Run("end session revokes and redirects", func(t *testing.T) {
		q := url.Values{"id_token_hint": {idToken}, "post_logout_redirect_uri": {logoutURI}, "state": {"bye"}}
		res, err := noFollow.Get(server.URL + "/auth/oauth2/logout?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		loc, _ := res.Location()
		if res.StatusCode != http.StatusFound || loc == nil || loc.Query().Get("state") != "bye" || !strings.HasPrefix(loc.String(), logoutURI) {
			t.Fatalf("logout status %d location %v", res.StatusCode, loc)
		}

		// the refresh family of the session is revoked
		claims := decodeJWTPayloadForTest(t, idToken)
		sid, _ := claims["sid"].(string)
		if revoked, err := m.TokenClaimsRevoked(ctx, "", sid); err != nil || !revoked {
			t.Fatalf("session revoked = %v, %v", revoked, err)
		}
	})

	t.Run("end session refuses unregistered redirect", func(t *testing.T) {
		q := url.Values{"id_token_hint": {idToken}, "post_logout_redirect_uri": {"https://evil.example/"}}
		res, err := noFollow.Get(server.URL + "/auth/oauth2/logout?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})

	t.Run("end session without hint shows a page", func(t *testing.T) {
		res, err := noFollow.Get(server.URL + "/auth/oauth2/logout")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
	})
}
