package login

import (
	"fmt"
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/auth"
)

func (m *Login) SetState(w http.ResponseWriter, state string) {
	auth.SetCookie(w, state, &m.StateCookie)
}

func (m *Login) GetState(r *http.Request) (string, error) {
	cookie, err := r.Cookie(m.StateCookie.CookieName)
	if err != nil {
		return "", err
	}

	return cookie.Value, nil
}

func (m *Login) CheckState(w http.ResponseWriter, r *http.Request, check string) error {
	state, err := m.GetState(r)
	if err != nil {
		return err
	}

	m.RemoveState(w)

	if state != check {
		return fmt.Errorf("state is not valid")
	}

	return nil
}

func (m *Login) RemoveState(w http.ResponseWriter) {
	auth.RemoveCookie(w, &m.StateCookie)
}
