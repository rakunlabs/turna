package middlewares

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/labstack/echo/v4"
)

type RedirectionContinue struct {
	Redirects []Redirect `cfg:"redirects"`

	Permanent bool `cfg:"permanent"`
}

type Redirect struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`

	rgx *regexp.Regexp
}

func (m *RedirectionContinue) Middleware() (echo.MiddlewareFunc, error) {
	if len(m.Redirects) == 0 {
		return nil, fmt.Errorf("redirects is empty")
	}

	for i, r := range m.Redirects {
		if r.Regex == "" {
			return nil, fmt.Errorf("redirects[%d].regex is empty", i)
		}

		rgx, err := regexp.Compile(r.Regex)
		if err != nil {
			return nil, fmt.Errorf("redirects[%d].regex invalid: %w", i, err)
		}

		m.Redirects[i].rgx = rgx
	}

	statusCode := http.StatusTemporaryRedirect
	if m.Permanent {
		statusCode = http.StatusPermanentRedirect
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var u url.URL
			request := c.Request()
			u.Path = request.URL.Path
			u.RawQuery = request.URL.RawQuery
			u.RawFragment = request.URL.RawFragment

			oldPath := u.String()

			for _, r := range m.Redirects {
				newPath := r.rgx.ReplaceAllString(oldPath, r.Replacement)

				if oldPath != newPath {
					return c.Redirect(statusCode, newPath)
				}
			}

			return next(c)
		}
	}, nil
}
