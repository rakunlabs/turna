package login

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type rememberIssuer struct {
	form url.Values
}

func (i *rememberIssuer) Keyfunc(_ *jwt.Token) (any, error) {
	return nil, session.ErrKIDNotFound
}

func (i *rememberIssuer) IssueToken(_ *http.Request, form url.Values) ([]byte, int, error) {
	i.form = form

	return []byte(`{"access_token":"token"}`), http.StatusOK, nil
}

func TestIssuerPasswordTokenCarriesRememberMe(t *testing.T) {
	issuer := &rememberIssuer{}
	session.IssuerRegistry.Set("remember-auth", issuer)
	m := &Login{}
	r := httptest.NewRequest(http.MethodPost, "https://example.com/login", nil)

	_, status, err := m.IssuerPasswordToken(r, "remember-auth", "user", "secret", true, &session.Oauth2{ClientID: "ui"})
	if err != nil || status != http.StatusOK {
		t.Fatalf("password token: status=%d err=%v", status, err)
	}
	if got := issuer.form.Get("remember_me"); got != "true" {
		t.Fatalf("remember_me = %q", got)
	}
}
