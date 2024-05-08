package login

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
)

// NewState generates cryptographically secure random state with base64 URL encoding.
func NewState() (string, error) {
	cryptoRandBytes := make([]byte, 16)
	_, err := rand.Read(cryptoRandBytes)
	if err != nil {
		return "", err
	}

	base64State := strings.TrimRight(base64.URLEncoding.EncodeToString(cryptoRandBytes), "=")

	return base64State, nil
}

func (m *Login) SetState(c echo.Context, state string) {
	SetCookie(c.Response(), state, &m.StateCookie)
}

func (m *Login) GetState(c echo.Context) (string, error) {
	cookie, err := c.Cookie(m.StateCookie.CookieName)
	if err != nil {
		return "", err
	}

	return cookie.Value, nil
}

func (m *Login) CheckState(c echo.Context, check string) error {
	state, err := m.GetState(c)
	if err != nil {
		return err
	}

	m.RemoveState(c)

	if state != check {
		return fmt.Errorf("state is not valid")
	}

	return nil
}

func (m *Login) RemoveState(c echo.Context) {
	RemoveCookie(c.Response(), &m.StateCookie)
}
