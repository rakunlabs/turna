package session

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/ada/utils/sessions"
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
