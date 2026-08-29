package login

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	sessionstore "github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

type enrollmentTestIssuer struct {
	public    *rsa.PublicKey
	userID    string
	method    string
	registers int
}

func (i *enrollmentTestIssuer) Keyfunc(*jwt.Token) (any, error) {
	return i.public, nil
}

func (i *enrollmentTestIssuer) IssueToken(*http.Request, url.Values) ([]byte, int, error) {
	return nil, http.StatusNotImplemented, nil
}

func (i *enrollmentTestIssuer) PasskeyEnrollmentStatus(_ context.Context, userID, method string) (session.PasskeyEnrollmentStatus, error) {
	i.userID = userID
	i.method = method

	return session.PasskeyEnrollmentStatus{Prompt: true, PromptID: "opaque", SnoozeSeconds: 60}, nil
}

func (i *enrollmentTestIssuer) PasskeyEnrollmentRegister(_ context.Context, _ *http.Request, userID, method string, _ []byte) ([]byte, int, error) {
	i.userID = userID
	i.method = method
	i.registers++

	return []byte(`{"payload":{"message":"registered"}}`), http.StatusOK, nil
}

func TestPasskeyEnrollmentUsesValidatedSessionIdentity(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issuer := &enrollmentTestIssuer{public: &private.PublicKey}
	const issuerName = "login-enrollment-test-auth"
	const sessionName = "login-enrollment-test-session"
	session.IssuerRegistry.Set(issuerName, issuer)

	sessionM := &session.Session{
		CookieName: "auth_session",
		Store: session.Store{File: &sessionstore.File{
			SessionKey: "login-enrollment-test-key",
			Path:       t.TempDir(),
		}},
		Action: session.Action{Token: &session.Token{DisableRefresh: true}},
		Provider: map[string]session.Provider{
			"auth": {AuthMiddleware: issuerName, Oauth2: &session.Oauth2{}},
		},
	}
	if _, err := sessionM.Middleware(context.Background(), sessionName); err != nil {
		t.Fatalf("session middleware: %v", err)
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "user-from-signed-token",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(private)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	tokenBody, _ := json.Marshal(session.TokenData{AccessToken: accessToken})
	loginResponse := httptest.NewRecorder()
	if err := sessionM.SetLoginToken(
		loginResponse,
		httptest.NewRequest(http.MethodPost, "https://example.com/login", nil),
		tokenBody,
		"auth",
		session.AuthenticationMethodCode,
	); err != nil {
		t.Fatalf("set login token: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]

	m := &Login{Path: Path{Base: "/login/"}, SessionMiddleware: sessionName, session: sessionM}
	middleware, err := m.Middleware(context.Background())
	if err != nil {
		t.Fatalf("login middleware: %v", err)
	}
	handler := middleware(http.NotFoundHandler())

	statusRequest := httptest.NewRequest(http.MethodGet, "https://example.com/login/auth/passkey/enrollment", nil)
	statusRequest.AddCookie(cookie)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status code = %d, body=%s", statusResponse.Code, statusResponse.Body.String())
	}
	if issuer.userID != "user-from-signed-token" || issuer.method != session.AuthenticationMethodCode {
		t.Fatalf("issuer identity = %q method = %q", issuer.userID, issuer.method)
	}

	registerRequest := httptest.NewRequest(http.MethodPost, "https://example.com/login/auth/passkey/enrollment", nil)
	registerRequest.AddCookie(cookie)
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, registerRequest)
	if registerResponse.Code != http.StatusOK || issuer.registers != 1 {
		t.Fatalf("register code = %d calls=%d body=%s", registerResponse.Code, issuer.registers, registerResponse.Body.String())
	}
}
