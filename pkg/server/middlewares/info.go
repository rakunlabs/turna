package middlewares

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Info struct {
	// Cookie to show.
	Cookie string `cfg:"cookie"`
	// Base64 decode when reading cookie.
	Base64 bool `cfg:"base64"`
}

func (s *Info) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie(s.Cookie)
			if err != nil {
				return c.String(http.StatusNotFound, fmt.Sprintf("Cookie %s not found", s.Cookie))
			}

			v := []byte(cookie.Value)
			if s.Base64 {
				var err error
				v, err = base64.StdEncoding.DecodeString(cookie.Value)
				if err != nil {
					return c.String(http.StatusBadRequest, err.Error())
				}
			}

			return c.JSONPretty(http.StatusOK, json.RawMessage(v), "  ")
		}
	}
}
