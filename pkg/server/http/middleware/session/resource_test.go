package session

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIssuerURLIssuer is a fakeIssuer that also knows its external issuer
// URL like the auth middleware (session.InfIssuerURL).
type fakeIssuerURLIssuer struct {
	fakeIssuer

	issuerURL string
}

func (f *fakeIssuerURLIssuer) IssuerURL(_ *http.Request) string {
	return f.issuerURL
}

func newResourceSession(t *testing.T, key *rsa.PrivateKey, pr *ProtectedResource, redirectAlways bool) *Session {
	t.Helper()

	IssuerRegistry.Set("resource-auth", &fakeIssuerURLIssuer{
		fakeIssuer: fakeIssuer{kid: "kid-1", key: &key.PublicKey},
		issuerURL:  "https://idp.example.com/auth/oauth2",
	})

	m := &Session{
		Provider: map[string]Provider{
			"turna": {AuthMiddleware: "resource-auth"},
		},
		Action: Action{
			Token: &Token{
				LoginPath:      "/login/",
				RedirectAlways: redirectAlways,
			},
		},
		ProtectedResource: pr,
		store:             fakeStore{},
	}

	if err := m.SetAction(); err != nil {
		t.Fatalf("set action: %v", err)
	}

	return m
}

func TestRedirectOnlyForHTML(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next must not be called")
	})

	do := func(m *Session, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/private/page", nil)
		for k, v := range header {
			r.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		m.Do(next, rec, r)

		return rec
	}

	m := newResourceSession(t, key, nil, false)

	t.Run("browser accept header redirects to login", func(t *testing.T) {
		rec := do(m, map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"})
		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "redirect_path=%2Fprivate%2Fpage") {
			t.Errorf("location = %q", loc)
		}
	})

	t.Run("browser navigation without accept redirects", func(t *testing.T) {
		rec := do(m, map[string]string{"Sec-Fetch-Dest": "document"})
		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})

	t.Run("machine client answers 401 challenge", func(t *testing.T) {
		rec := do(m, map[string]string{"Accept": "application/json, text/event-stream"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
		}
	})

	t.Run("no accept header answers 401 challenge", func(t *testing.T) {
		rec := do(m, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("redirect_always keeps historic behavior", func(t *testing.T) {
		legacy := newResourceSession(t, key, nil, true)

		rec := do(legacy, map[string]string{"Accept": "application/json"})
		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})
}

func TestProtectedResource(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	do := func(m *Session, target string, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		for k, v := range header {
			r.Header.Set(k, v)
		}

		rec := httptest.NewRecorder()
		m.Do(next, rec, r)

		return rec
	}

	t.Run("metadata document is public with derived authorization server", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{ScopesSupported: []string{"mcp"}}, false)

		rec := do(m, "https://app.example.com/.well-known/oauth-protected-resource", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var doc struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
			ScopesSupported      []string `json:"scopes_supported"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if doc.Resource != "https://app.example.com" {
			t.Errorf("resource = %q", doc.Resource)
		}
		if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://idp.example.com/auth/oauth2" {
			t.Errorf("authorization_servers = %v", doc.AuthorizationServers)
		}
		if len(doc.ScopesSupported) != 1 || doc.ScopesSupported[0] != "mcp" {
			t.Errorf("scopes_supported = %v", doc.ScopesSupported)
		}
	})

	t.Run("configured values override derivation", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{
			Resource:             "https://mcp.example.com/mcp",
			AuthorizationServers: []string{"https://other-idp.example.com/oauth2"},
		}, false)

		rec := do(m, "https://mcp.example.com/.well-known/oauth-protected-resource/mcp", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var doc struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if doc.Resource != "https://mcp.example.com/mcp" {
			t.Errorf("resource = %q", doc.Resource)
		}
		if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://other-idp.example.com/oauth2" {
			t.Errorf("authorization_servers = %v", doc.AuthorizationServers)
		}
	})

	t.Run("challenge carries path-inserted resource_metadata", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{}, false)

		rec := do(m, "https://app.example.com/krabby/mcp", map[string]string{"Accept": "application/json, text/event-stream"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}

		want := `Bearer resource_metadata="https://app.example.com/.well-known/oauth-protected-resource/krabby/mcp"`
		if got := rec.Header().Get("WWW-Authenticate"); got != want {
			t.Errorf("WWW-Authenticate = %q, want %q", got, want)
		}
	})

	t.Run("per-path metadata documents describe their own resource", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{}, false)

		for _, path := range []string{"/krabby/mcp", "/krabby/mcp/admin"} {
			rec := do(m, "https://app.example.com/.well-known/oauth-protected-resource"+path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			var doc struct {
				Resource             string   `json:"resource"`
				AuthorizationServers []string `json:"authorization_servers"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if want := "https://app.example.com" + path; doc.Resource != want {
				t.Errorf("resource = %q, want %q", doc.Resource, want)
			}
			if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://idp.example.com/auth/oauth2" {
				t.Errorf("authorization_servers = %v", doc.AuthorizationServers)
			}
		}
	})

	t.Run("invalid bearer challenge carries resource_metadata", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{}, false)

		rec := do(m, "https://app.example.com/mcp", map[string]string{"Authorization": "Bearer garbage"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}

		if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "resource_metadata=") {
			t.Errorf("WWW-Authenticate = %q, want resource_metadata pointer", got)
		}
	})

	t.Run("path resource builds path-insertion metadata url", func(t *testing.T) {
		m := newResourceSession(t, key, &ProtectedResource{Resource: "https://app.example.com/mcp"}, false)

		rec := do(m, "https://app.example.com/mcp", map[string]string{"Accept": "application/json"})
		want := `Bearer resource_metadata="https://app.example.com/.well-known/oauth-protected-resource/mcp"`
		if got := rec.Header().Get("WWW-Authenticate"); got != want {
			t.Errorf("WWW-Authenticate = %q, want %q", got, want)
		}
	})

	t.Run("audience enforcement is scoped by azp", func(t *testing.T) {
		const claudeAZP = "https://claude.ai/oauth/claude-code-client-metadata"

		token := func(azp string, aud any) string {
			return signSkipToken(t, key, jwt.MapClaims{
				"preferred_username": "user1",
				"azp":                azp,
				"aud":                aud,
			})
		}

		// Empty list disables audience enforcement even for Claude.
		m := newResourceSession(t, key, &ProtectedResource{}, false)
		rec := do(m, "https://app.example.com/krabby/mcp", map[string]string{
			"Authorization": "Bearer " + token(claudeAZP, "turna-auth"),
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("empty list status = %d, want 200", rec.Code)
		}

		m = newResourceSession(t, key, &ProtectedResource{CheckAudienceAZP: []string{claudeAZP}}, false)

		// A listed client must carry the exact requested resource in aud.
		rec = do(m, "https://app.example.com/krabby/mcp", map[string]string{
			"Authorization": "Bearer " + token(claudeAZP, "turna-auth"),
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("listed azp with wrong aud status = %d, want 401", rec.Code)
		}
		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(challenge, `error="invalid_token"`) ||
			!strings.Contains(challenge, `resource_metadata="https://app.example.com/.well-known/oauth-protected-resource/krabby/mcp"`) {
			t.Errorf("WWW-Authenticate = %q", challenge)
		}

		rec = do(m, "https://app.example.com/krabby/mcp", map[string]string{
			"Authorization": "Bearer " + token(claudeAZP, []string{"turna-auth", "https://app.example.com/krabby/mcp"}),
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("listed azp with correct aud status = %d, want 200", rec.Code)
		}

		// Other clients continue to iam_check/downstream without an aud gate.
		rec = do(m, "https://app.example.com/krabby/mcp", map[string]string{
			"Authorization": "Bearer " + token("browser-client", "turna-auth"),
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("unlisted azp status = %d, want 200", rec.Code)
		}
	})

	t.Run("without protected_resource the well-known path stays protected", func(t *testing.T) {
		m := newResourceSession(t, key, nil, false)

		rec := do(m, "https://app.example.com/.well-known/oauth-protected-resource", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}
