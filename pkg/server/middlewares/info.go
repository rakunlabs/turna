package middlewares

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/pkg/authecho"
)

type Info struct {
	// Cookie to show.
	Cookie string `cfg:"cookie"`
	// Session cookie.
	Session bool `cfg:"session"`
	// SessionStoreName to get from auth_echo.
	SessionStoreName string `cfg:"session_store_name"`
	// SessionValueName to get from session, default is "cookie".
	SessionValueName string `cfg:"session_value_name"`
	// Base64 decode when reading cookie.
	Base64 bool `cfg:"base64"`
	// Raw cookie to show
	Raw bool `cfg:"raw"`
}

func (s *Info) Middleware() echo.MiddlewareFunc {
	if s.SessionValueName == "" {
		s.SessionValueName = "cookie"
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var retValue []byte

			if s.Session {
				sessionStore := authecho.Store.GetSessionFilesystem(s.SessionStoreName)
				if sessionStore == nil {
					return c.String(http.StatusNotFound, fmt.Sprintf("Session %s not found", s.SessionStoreName))
				}

				if vGet, err := sessionStore.Get(c.Request(), s.Cookie); !vGet.IsNew && err == nil {
					// add the access token to the request
					vStr, _ := vGet.Values[s.SessionValueName].(string)
					retValue = []byte(vStr)
				}
			} else {
				cookie, err := c.Cookie(s.Cookie)
				if err != nil {
					return c.String(http.StatusNotFound, fmt.Sprintf("Cookie %s not found", s.Cookie))
				}

				retValue = []byte(cookie.Value)
			}

			if s.Base64 {
				var err error
				retValue, err = base64.StdEncoding.DecodeString(string(retValue))
				if err != nil {
					return c.String(http.StatusBadRequest, err.Error())
				}
			}

			if s.Raw {
				return c.String(http.StatusOK, string(retValue))
			}

			return c.JSONPretty(http.StatusOK, json.RawMessage(retValue), "  ")
		}
	}
}
