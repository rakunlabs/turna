package session

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/ada/utils/sessions"

	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

// fakeStore answers every session lookup as anonymous (no cookie).
type fakeStore struct{}

func (fakeStore) Get(_ *http.Request, name string) (*sessions.Session, error) {
	return &sessions.Session{IsNew: true, Values: map[string]any{}}, nil
}

func newSkipSession(t *testing.T, key *rsa.PrivateKey, skip []string, legacy bool) *Session {
	t.Helper()

	IssuerRegistry.Set("skip-auth", &fakeIssuer{kid: "kid-1", key: &key.PublicKey})

	m := &Session{
		Provider: map[string]Provider{
			"turna": {AuthMiddleware: "skip-auth"},
		},
		Action: Action{
			Token: &Token{
				LoginPath:       "/login/",
				LegacyProxyAuth: legacy,
			},
		},
		SkipPaths: skip,
		store:     fakeStore{},
	}

	if err := m.SetAction(); err != nil {
		t.Fatalf("set action: %v", err)
	}

	return m
}

func signSkipToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()

	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Minute).Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "kid-1"

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	return signed
}

func TestSkipPaths(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	m := newSkipSession(t, key, []string{"/pub/**"}, false)

	var gotUser string
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotUser = r.Header.Get("X-User")
		w.WriteHeader(http.StatusOK)
	})

	do := func(path, bearer string) *httptest.ResponseRecorder {
		called, gotUser = false, ""

		r := httptest.NewRequest(http.MethodGet, path, nil)
		// spoofing attempt: must never survive
		r.Header.Set("X-User", "spoofed")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}

		rec := httptest.NewRecorder()
		m.Do(next, rec, r)

		return rec
	}

	t.Run("anonymous non-skip redirects to login", func(t *testing.T) {
		rec := do("/private/page", "")
		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "redirect_path=%2Fprivate%2Fpage") {
			t.Errorf("location = %q", loc)
		}
		if called {
			t.Error("next must not be called")
		}
	})

	t.Run("anonymous skip passes through stripped", func(t *testing.T) {
		rec := do("/pub/oauth2/token", "")
		if rec.Code != http.StatusOK || !called {
			t.Fatalf("status = %d called = %v", rec.Code, called)
		}
		if gotUser != "" {
			t.Errorf("X-User = %q, want empty (spoof stripped)", gotUser)
		}
	})

	t.Run("valid bearer on skip path authenticates", func(t *testing.T) {
		token := signSkipToken(t, key, jwt.MapClaims{"preferred_username": "user1"})

		rec := do("/pub/oauth2/consent", token)
		if rec.Code != http.StatusOK || !called {
			t.Fatalf("status = %d called = %v", rec.Code, called)
		}
		if gotUser != "user1" {
			t.Errorf("X-User = %q, want user1", gotUser)
		}
	})

	t.Run("invalid bearer on skip path stays anonymous", func(t *testing.T) {
		rec := do("/pub/oauth2/token", "garbage")
		if rec.Code != http.StatusOK || !called {
			t.Fatalf("status = %d called = %v", rec.Code, called)
		}
		if gotUser != "" {
			t.Errorf("X-User = %q, want empty", gotUser)
		}
	})

	t.Run("refresh token on skip path stays anonymous", func(t *testing.T) {
		token := signSkipToken(t, key, jwt.MapClaims{"preferred_username": "user1", "typ": "Refresh"})

		rec := do("/pub/oauth2/token", token)
		if rec.Code != http.StatusOK || !called {
			t.Fatalf("status = %d called = %v", rec.Code, called)
		}
		if gotUser != "" {
			t.Errorf("X-User = %q, want empty", gotUser)
		}
	})

	t.Run("invalid bearer on non-skip path answers 401", func(t *testing.T) {
		rec := do("/private/page", "garbage")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("WWW-Authenticate = %q", rec.Header().Get("WWW-Authenticate"))
		}
	})
}

// fakePublicPathsIssuer is a fakeIssuer that also publishes a public plane.
type fakePublicPathsIssuer struct {
	fakeIssuer

	patterns []string
}

func (f *fakePublicPathsIssuer) PublicPathPatterns() []string {
	return f.patterns
}

func TestAuthSkipPaths(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	newSession := func(authSkipPaths []string) *Session {
		IssuerRegistry.Set("public-auth", &fakePublicPathsIssuer{
			fakeIssuer: fakeIssuer{kid: "kid-1", key: &key.PublicKey},
			patterns:   []string{"/auth/oauth2/**", "/.well-known/openid-configuration"},
		})

		m := &Session{
			Provider: map[string]Provider{
				"turna": {AuthMiddleware: "public-auth"},
			},
			Action: Action{
				Token: &Token{LoginPath: "/login/"},
			},
			AuthSkipPaths: authSkipPaths,
			store:         fakeStore{},
		}

		if err := m.SetAction(); err != nil {
			t.Fatalf("set action: %v", err)
		}

		return m
	}

	withSkip := []string{"public-auth"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	do := func(m *Session, path, authorization string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}

		rec := httptest.NewRecorder()
		m.Do(next, rec, r)

		return rec
	}

	t.Run("auth_skip_paths exposes the issuer public plane", func(t *testing.T) {
		if rec := do(newSession(withSkip), "/auth/oauth2/token", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("federated callback is not captured by login redirect", func(t *testing.T) {
		if rec := do(newSession(withSkip), "/auth/oauth2/code/gitlab", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("basic client authentication survives the public plane", func(t *testing.T) {
		// the token endpoint receives Authorization: Basic <client>; the
		// session middleware must not answer 401 trying to parse it as a JWT
		if rec := do(newSession(withSkip), "/auth/oauth2/token", "Basic dHVybmE6c2VjcmV0"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("discovery document is reachable", func(t *testing.T) {
		if rec := do(newSession(withSkip), "/.well-known/openid-configuration", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("other paths still require authentication", func(t *testing.T) {
		if rec := do(newSession(withSkip), "/private/page", ""); rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})

	t.Run("provider auth_middleware alone adds no skip paths", func(t *testing.T) {
		if rec := do(newSession(nil), "/auth/oauth2/token", ""); rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})

	t.Run("unknown auth middleware name is skipped", func(t *testing.T) {
		if rec := do(newSession([]string{"no-such-auth"}), "/auth/oauth2/token", ""); rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})
}

// fakePublicCheckIssuer is a fakeIssuer that also answers anonymous public
// permission checks (session.InfAccessChecker) like the auth middleware.
type fakePublicCheckIssuer struct {
	fakeIssuer

	public map[string]bool // path -> flagged public
}

func (f *fakePublicCheckIssuer) AccessAllowed(_ context.Context, alias, _, path, _ string) (bool, error) {
	if alias != "" {
		return false, nil
	}

	return f.public[path], nil
}

func TestAuthPublicCheck(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	IssuerRegistry.Set("check-auth", &fakePublicCheckIssuer{
		fakeIssuer: fakeIssuer{kid: "kid-1", key: &key.PublicKey},
		public:     map[string]bool{"/docs/page": true},
	})

	m := &Session{
		Provider: map[string]Provider{
			"turna": {AuthMiddleware: "check-auth"},
		},
		Action: Action{
			Token: &Token{LoginPath: "/login/"},
		},
		SkipPaths:     []string{"/pub/**"},
		AuthSkipPaths: []string{"check-auth"},
		store:         fakeStore{},
	}

	if err := m.SetAction(); err != nil {
		t.Fatalf("set action: %v", err)
	}

	var gotUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User")
		w.WriteHeader(http.StatusOK)
	})

	do := func(path, bearer string) (*httptest.ResponseRecorder, *tcontext.Turna) {
		gotUser = ""

		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-User", "spoofed")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}

		rec := httptest.NewRecorder()
		turna, r := tcontext.New(rec, r)
		m.Do(next, rec, r)

		return rec, turna
	}

	publicFlag := func(turna *tcontext.Turna) bool {
		v, _ := turna.GetInterface(CtxPublicAccessKey).(bool)

		return v
	}

	t.Run("anonymous public permission path passes stripped", func(t *testing.T) {
		rec, turna := do("/docs/page", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if gotUser != "" {
			t.Errorf("X-User = %q, want empty (spoof stripped)", gotUser)
		}
		if !publicFlag(turna) {
			t.Error("public_access context flag must be set for iam_check")
		}
	})

	t.Run("valid bearer on public path authenticates", func(t *testing.T) {
		token := signSkipToken(t, key, jwt.MapClaims{"preferred_username": "user1"})

		rec, turna := do("/docs/page", token)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if gotUser != "user1" {
			t.Errorf("X-User = %q, want user1", gotUser)
		}
		if !publicFlag(turna) {
			t.Error("public_access context flag must be set for iam_check")
		}
	})

	t.Run("non-public path still redirects to login", func(t *testing.T) {
		rec, turna := do("/private/page", "")
		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
		if publicFlag(turna) {
			t.Error("public_access context flag must not be set")
		}
	})

	t.Run("pattern skip does not flag public", func(t *testing.T) {
		rec, turna := do("/pub/page", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if publicFlag(turna) {
			t.Error("skip_paths match must not set public_access; iam_check still decides")
		}
	})
}

func TestAuthPublicCheckRemote(t *testing.T) {
	// mimic the auth middleware's <prefix>/check endpoint: anonymous body,
	// public match answers {"allowed":true}, everything else 401.
	checkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if xUser := r.Header.Get("X-User"); xUser != "" {
			t.Errorf("X-User = %q, must be anonymous", xUser)
		}

		var body struct {
			Host   string `json:"host"`
			Path   string `json:"path"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}

		if body.Path == "/docs/page" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"allowed":true}`))

			return
		}

		http.Error(w, `{"error":"X-User header is required"}`, http.StatusUnauthorized)
	}))
	defer checkServer.Close()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	IssuerRegistry.Set("remote-test-auth", &fakeIssuer{kid: "kid-1", key: &key.PublicKey})

	m := &Session{
		Provider: map[string]Provider{
			"turna": {AuthMiddleware: "remote-test-auth"},
		},
		Action: Action{
			Token: &Token{LoginPath: "/login/"},
		},
		AuthSkipPaths: []string{checkServer.URL},
		store:         fakeStore{},
	}

	if err := m.SetAction(); err != nil {
		t.Fatalf("set action: %v", err)
	}
	if err := m.initAuthSkip(); err != nil {
		t.Fatalf("init auth skip: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	do := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		m.Do(next, rec, r)

		return rec
	}

	t.Run("remote public path passes anonymously", func(t *testing.T) {
		if rec := do("/docs/page"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("remote non-public path redirects to login", func(t *testing.T) {
		if rec := do("/private/page"); rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", rec.Code)
		}
	})
}

func TestLegacyProxyAuth(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	m := newSkipSession(t, key, nil, true)

	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.Header.Set("Authorization", "Bearer garbage")

	rec := httptest.NewRecorder()
	m.Do(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next must not be called")
	}), rec, r)

	if rec.Code != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want 407 with legacy_proxy_auth", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("legacy mode must not set WWW-Authenticate")
	}
}
