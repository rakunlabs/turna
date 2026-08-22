package iamcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type fakeAccessIssuer struct {
	allowed bool
	alias   string
	path    string
}

func (f *fakeAccessIssuer) Keyfunc(*jwt.Token) (any, error) {
	return nil, nil
}

func (f *fakeAccessIssuer) IssueToken(*http.Request, url.Values) ([]byte, int, error) {
	return nil, http.StatusNotImplemented, nil
}

func (f *fakeAccessIssuer) AccessAllowed(_ context.Context, alias, _, path, _ string) (bool, error) {
	f.alias = alias
	f.path = path

	return f.allowed, nil
}

func TestAnonymousRequestUsesCentralPublicCheck(t *testing.T) {
	var got data.CheckRequest
	var gotXUser string
	checkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXUser = r.Header.Get("X-User")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode check request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data.CheckResponse{Allowed: got.Path == "/public"})
	}))
	defer checkServer.Close()

	m := &IamCheck{CheckAPI: checkServer.URL}
	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	called := false
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("public anonymous request passes", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.com/public", nil))

		if w.Code != http.StatusNoContent || !called {
			t.Fatalf("status = %d, called = %v", w.Code, called)
		}
		if got.Alias != "" || got.Path != "/public" || got.Method != http.MethodGet {
			t.Fatalf("check request = %+v", got)
		}
		if gotXUser != "" {
			t.Fatalf("X-User = %q, want empty", gotXUser)
		}
	})

	t.Run("private anonymous request gets 401", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.com/private", nil))

		if w.Code != http.StatusUnauthorized || called {
			t.Fatalf("status = %d, called = %v", w.Code, called)
		}
	})

	t.Run("denied authenticated request gets 403", func(t *testing.T) {
		called = false
		r := httptest.NewRequest(http.MethodGet, "http://example.com/private", nil)
		r.Header.Set("X-User", "user1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden || called {
			t.Fatalf("status = %d, called = %v", w.Code, called)
		}
		if got.Alias != "user1" {
			t.Fatalf("check alias = %q, want user1", got.Alias)
		}
		if gotXUser != "user1" {
			t.Fatalf("X-User = %q, want user1", gotXUser)
		}
	})
}

func TestAnonymousRequestAgainstLegacyCheckAPIStaysUnauthorized(t *testing.T) {
	checkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "alias or id is required", http.StatusBadRequest)
	}))
	defer checkServer.Close()

	m := &IamCheck{CheckAPI: checkServer.URL}
	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	w := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next must not be called")
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.com/private", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestCheckAPIErrorKeepsEndpointAndStatus(t *testing.T) {
	checkServer := httptest.NewServer(http.NotFoundHandler())
	defer checkServer.Close()

	m := &IamCheck{CheckAPI: checkServer.URL + "/wrong-check-path"}
	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "http://example.com/private", nil)
	r.Header.Set("X-User", "user1")
	w := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next must not be called")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", w.Code, w.Body.String())
	}
	for _, want := range []string{m.CheckAPI, "404 Not Found"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("body does not contain %q: %s", want, w.Body.String())
		}
	}
}

func TestAuthMiddlewareChecksInProcess(t *testing.T) {
	issuer := &fakeAccessIssuer{allowed: true}
	session.IssuerRegistry.Set("iamcheck-auth", issuer)

	m := &IamCheck{
		AuthMiddleware: "iamcheck-auth",
		// Must be ignored when the in-process issuer is configured.
		CheckAPI: "http://127.0.0.1:1/wrong",
	}
	middleware, err := m.Middleware()
	if err != nil {
		t.Fatalf("Middleware: %v", err)
	}

	called := false
	r := httptest.NewRequest(http.MethodGet, "http://example.com/public", nil)
	r.Header.Set("X-User", "user1")
	w := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)

	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("status = %d, called = %v", w.Code, called)
	}
	if issuer.alias != "user1" || issuer.path != "/public" {
		t.Fatalf("in-process check got alias=%q path=%q", issuer.alias, issuer.path)
	}
}
