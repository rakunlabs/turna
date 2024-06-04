package login

import "github.com/labstack/echo/v4"

func (m *Login) SetSuccess(c echo.Context, success string) {
	SetCookie(c.Response(), success, &m.SuccessCookie)
}

func (m *Login) GetSuccess(c echo.Context) (string, error) {
	cookie, err := c.Cookie(m.SuccessCookie.CookieName)
	if err != nil {
		return "", err
	}

	return cookie.Value, nil
}

func (m *Login) RemoveSuccess(c echo.Context) {
	RemoveCookie(c.Response(), &m.SuccessCookie)
}
