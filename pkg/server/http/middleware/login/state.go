package login

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
)

func (m *Login) SetState(w http.ResponseWriter, state string, rememberMe bool) {
	if rememberMe {
		state += ".remember"
	}

	auth.SetCookie(w, state, &m.StateCookie)
}

func (m *Login) GetState(r *http.Request) (string, bool, error) {
	cookie, err := r.Cookie(m.StateCookie.CookieName)
	if err != nil {
		return "", false, err
	}

	state, rememberMe := strings.CutSuffix(cookie.Value, ".remember")

	return state, rememberMe, nil
}

func (m *Login) CheckState(w http.ResponseWriter, r *http.Request, check string) (bool, error) {
	state, rememberMe, err := m.GetState(r)
	if err != nil {
		return false, err
	}

	m.RemoveState(w)

	if state != check {
		return false, fmt.Errorf("state is not valid")
	}

	return rememberMe, nil
}

func (m *Login) RemoveState(w http.ResponseWriter) {
	auth.RemoveCookie(w, &m.StateCookie)
}
