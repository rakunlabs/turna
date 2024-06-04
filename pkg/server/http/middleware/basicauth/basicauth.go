package basicauth

import (
	"fmt"
	"strings"

	goauth "github.com/abbot/go-http-auth"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type BasicAuth struct {
	Users []string `cfg:"users"`
	Realm string   `cfg:"realm"`
	// HeaderField is the name of the header field to set with the username
	HeaderField string `cfg:"header_field"`
	// RemoveHeader removes the Authorization header from the request
	RemoveHeader bool `cfg:"remove_header"`
}

func (b *BasicAuth) Middleware(name string) ([]echo.MiddlewareFunc, error) {
	v := make(map[string]string, len(b.Users))
	for _, user := range b.Users {
		parts := strings.Split(user, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid user: %q", user)
		}

		v[parts[0]] = parts[1]
	}

	realm := b.Realm
	if realm == "" {
		realm = name
	}

	gAuth := goauth.BasicAuth{
		Realm: realm,
		Secrets: func(user, realm string) string {
			if hash, ok := v[user]; ok {
				return hash
			}

			return ""
		},
	}

	config := middleware.BasicAuthConfig{
		Realm: realm,
		Validator: func(username, password string, c echo.Context) (bool, error) {
			if secret := gAuth.Secrets(username, realm); secret == "" || !goauth.CheckSecret(password, secret) {
				return false, nil
			}

			if b.RemoveHeader {
				c.Request().Header.Del("Authorization")
			}

			if b.HeaderField != "" {
				c.Request().Header.Set(b.HeaderField, username)
			}

			return true, nil
		},
	}

	return []echo.MiddlewareFunc{middleware.BasicAuthWithConfig(config)}, nil
}
