package middlewares

import (
	"fmt"
	"regexp"

	"github.com/labstack/echo/v4"
)

type RegexPath struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`
}

func (m *RegexPath) Middleware() ([]echo.MiddlewareFunc, error) {
	rgx, err := regexp.Compile(m.Regex)
	if err != nil {
		return nil, fmt.Errorf("regexPath invalid regex: %s", err)
	}

	return []echo.MiddlewareFunc{func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Request().URL.Path = rgx.ReplaceAllString(c.Request().URL.Path, m.Replacement)

			return next(c)
		}
	}}, nil
}
