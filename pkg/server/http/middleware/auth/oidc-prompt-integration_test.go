package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// TestOIDCPromptMaxAgeIntegration covers the OIDC prompt and max_age
// authorization request parameters and the auth_time carried into the
// id_token. Runs against a real PostgreSQL when AUTH_TEST_DSN is set.
func TestOIDCPromptMaxAgeIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "integration-test-key"}}
	middleware, err := m.Middleware(ctx, "auth-oidc-prompt")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	const (
		clientID    = "it-prompt-client"
		secret      = "it-prompt-secret"
		redirectURI = "https://app.example.com/cb"
		alias       = "it-prompt-user"
	)

	for _, put := range []struct{ ns, value string }{
		{"authorize", `{"login_url":"/login/"}`},
	} {
		if _, err := m.store.PutSetting(ctx, put.ns, json.RawMessage(put.value), "it"); err != nil {
			t.Fatal(err)
		}
	}
	clientCfg := func(skipConsent bool) json.RawMessage {
		raw, _ := json.Marshal(AccessClient{ClientSecret: secret, Scope: []string{"openid"}, RedirectURIs: []string{redirectURI}, SkipConsent: skipConsent})

		return raw
	}
	if _, err := m.store.PutOAuthClient(ctx, clientID, clientCfg(true), true, "it"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.PutOAuthClient(ctx, clientID+"-consent", clientCfg(false), true, "it"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = m.store.DeleteOAuthClient(context.Background(), clientID)
		_, _ = m.store.DeleteOAuthClient(context.Background(), clientID+"-consent")
	})

	server := httptest.NewServer(middleware(http.NotFoundHandler()))
	defer server.Close()

	{
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/auth/v1/users",
			strings.NewReader(`{"alias":["`+alias+`"],"local":true,"details":{"name":"Prompt User"},"is_active":true}`))
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

	// authorize runs the request as a browser with the given session
	// identity and authentication time; it returns the final redirect.
	authorize := func(t *testing.T, client string, params url.Values, user string, authTime int64) *url.URL {
		t.Helper()

		params.Set("client_id", client)
		params.Set("redirect_uri", redirectURI)
		params.Set("response_type", "code")
		params.Set("scope", "openid")
		params.Set("state", "s1")

		target := server.URL + "/auth/oauth2/authorize?" + params.Encode()
		for range 5 {
			req, _ := http.NewRequest(http.MethodGet, target, nil)
			if user != "" {
				req.Header.Set("X-User", user)
			}
			if authTime > 0 {
				req.Header.Set(session.HeaderAuthTime, strconv.FormatInt(authTime, 10))
			}

			res, err := noFollow.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()

			loc, err := res.Location()
			if err != nil {
				t.Fatalf("status %d without redirect", res.StatusCode)
			}
			if !strings.HasPrefix(loc.String(), server.URL) && loc.Host != "" {
				return loc
			}
			if strings.HasPrefix(loc.Path, "/login/") {
				return loc
			}
			target = server.URL + loc.RequestURI()
		}

		t.Fatal("too many redirects")

		return nil
	}

	idTokenAuthTime := func(t *testing.T, code string) int64 {
		t.Helper()

		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI},
			"client_id": {clientID}, "client_secret": {secret}}
		res, err := http.PostForm(server.URL+"/auth/oauth2/token", form)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		var tok AccessTokenResponse
		if err := json.NewDecoder(res.Body).Decode(&tok); err != nil || tok.IDToken == "" {
			t.Fatalf("token status %d: %v", res.StatusCode, err)
		}

		return claimInt64(decodeJWTPayloadForTest(t, tok.IDToken)["auth_time"])
	}

	now := time.Now().Unix()

	t.Run("prompt=none without session", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"prompt": {"none"}}, "", 0)
		if loc.Query().Get("error") != "login_required" || loc.Query().Get("state") != "s1" {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("prompt=none with session", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"prompt": {"none"}}, alias, now-60)
		if loc.Query().Get("code") == "" {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("prompt=none needs consent", func(t *testing.T) {
		loc := authorize(t, clientID+"-consent", url.Values{"prompt": {"none"}}, alias, now-60)
		if loc.Query().Get("error") != "consent_required" {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("prompt=none combined is invalid", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"prompt": {"none login"}}, alias, now)
		if loc.Query().Get("error") != "invalid_request" {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("prompt=login sends an old session to the login page", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"prompt": {"login"}}, alias, now-60)
		if !strings.HasPrefix(loc.Path, "/login/") || loc.Query().Get("prompt") != "login" {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("max_age expired re-authenticates", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"max_age": {"10"}}, alias, now-60)
		if !strings.HasPrefix(loc.Path, "/login/") {
			t.Fatalf("redirect = %s", loc)
		}
	})

	t.Run("max_age satisfied keeps auth_time", func(t *testing.T) {
		authTime := now - 30
		loc := authorize(t, clientID, url.Values{"max_age": {"10000"}}, alias, authTime)
		code := loc.Query().Get("code")
		if code == "" {
			t.Fatalf("redirect = %s", loc)
		}
		if got := idTokenAuthTime(t, code); got != authTime {
			t.Fatalf("id_token auth_time = %d, want %d", got, authTime)
		}
	})

	t.Run("invalid max_age", func(t *testing.T) {
		loc := authorize(t, clientID, url.Values{"max_age": {"-1"}}, alias, now)
		if loc.Query().Get("error") != "invalid_request" {
			t.Fatalf("redirect = %s", loc)
		}
	})
}
