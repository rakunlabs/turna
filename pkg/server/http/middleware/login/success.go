package login

import (
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/auth"
)

func (m *Login) SetSuccess(w http.ResponseWriter, success string) {
	auth.SetCookie(w, success, &m.SuccessCookie)
}

func (m *Login) GetSuccess(r http.Request) (string, error) {
	cookie, err := r.Cookie(m.SuccessCookie.CookieName)
	if err != nil {
		return "", err
	}

	return cookie.Value, nil
}

func (m *Login) RemoveSuccess(w http.ResponseWriter) {
	auth.RemoveCookie(w, &m.SuccessCookie)
}
