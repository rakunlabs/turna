package middlewares

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type Redirection struct {
	URL       string `cfg:"url"`
	Permanent bool   `cfg:"permanent"`
}

func (m *Redirection) Middleware() (echo.MiddlewareFunc, error) {
	statusCode := http.StatusTemporaryRedirect
	if m.URL == "" {
		m.URL = "/"
	}
	if m.Permanent {
		statusCode = http.StatusPermanentRedirect
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return c.Redirect(statusCode, m.URL)
		}
	}, nil
}
