package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type fakeCodeIssuer struct {
	codes map[string]store.Code
}

func (f *fakeCodeIssuer) Keyfunc(*jwt.Token) (any, error) { return nil, session.ErrKIDNotFound }

func (f *fakeCodeIssuer) IssueToken(*http.Request, url.Values) ([]byte, int, error) {
	return nil, http.StatusNotImplemented, nil
}

func (f *fakeCodeIssuer) IssueAuthorizationCode(_ context.Context, code store.Code) (string, error) {
	id := "issued-by-auth"
	f.codes[id] = code

	return id, nil
}

// response_type=code behind an in-process auth middleware must put the code
// in the auth middleware's store, where its token endpoint redeems it; the
// login store is a separate cache the token endpoint never reads.
func TestAuthCodeReturnUsesAuthMiddlewareCodeStore(t *testing.T) {
	issuer := &fakeCodeIssuer{codes: map[string]store.Code{}}
	session.IssuerRegistry.Set("login-test-auth", issuer)

	loginStore, err := (&store.Store{}).Init(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loginStore.Close() })

	m := &Login{
		store: loginStore,
		session: &session.Session{Provider: map[string]session.Provider{
			"local":    {AuthMiddleware: "login-test-auth"},
			"external": {},
		}},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/login?response_type=code&client_id=app&redirect_uri=https://app.example.com/cb&state=s", nil)
	m.AuthCodeReturn(recorder, request, &claims.Custom{Map: map[string]any{"preferred_username": "user"}})

	if recorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	location, _ := url.Parse(recorder.Header().Get("Location"))
	if got := location.Query().Get("code"); got != "issued-by-auth" {
		t.Fatalf("code = %q, want the auth middleware's code", got)
	}
	if code := issuer.codes["issued-by-auth"]; code.Alias != "user" || code.ClientID != "app" {
		t.Fatalf("issued code = %+v", code)
	}
	if _, ok, _ := loginStore.Code.Get(t.Context(), "code_issued-by-auth"); ok {
		t.Fatal("code must not be written to the login store")
	}
}

// An explicit auth_middleware links login to auth even when the session
// providers talk to it over HTTP and carry no auth_middleware themselves.
func TestAuthCodeReturnExplicitAuthMiddleware(t *testing.T) {
	issuer := &fakeCodeIssuer{codes: map[string]store.Code{}}
	session.IssuerRegistry.Set("login-test-explicit-auth", issuer)

	m := &Login{
		AuthMiddleware: "login-test-explicit-auth",
		session:        &session.Session{Provider: map[string]session.Provider{"http-only": {}}},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/login?response_type=code&client_id=app&redirect_uri=https://app.example.com/cb", nil)
	m.AuthCodeReturn(recorder, request, &claims.Custom{Map: map[string]any{"preferred_username": "user"}})

	if recorder.Code != http.StatusTemporaryRedirect || len(issuer.codes) != 1 {
		t.Fatalf("status = %d, issued = %d", recorder.Code, len(issuer.codes))
	}
}
