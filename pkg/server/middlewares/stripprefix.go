package middlewares

import (
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

type StripPrefix struct {
	// ForceSlash default is true
	ForceSlash *bool    `cfg:"force_slash"`
	Prefixes   []string `cfg:"prefixes"`
	Prefix     string   `cfg:"prefix"`
}

func (a *StripPrefix) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			prefixes := []string{a.Prefix}
			if len(a.Prefixes) > 0 {
				prefixes = a.Prefixes
			}

			// strip url path in prefixes
			urlPath := c.Request().URL.Path
			for _, prefix := range prefixes {
				if ok := strings.HasPrefix(urlPath, prefix); ok {
					urlPath = strings.TrimPrefix(urlPath, prefix)

					break
				}
			}

			forceSlash := true
			if a.ForceSlash != nil {
				forceSlash = *a.ForceSlash
			}

			// force slash
			if forceSlash {
				slashedPath, err := url.JoinPath("/", urlPath)
				if err != nil {
					return err
				}

				urlPath = slashedPath
			}

			c.Request().URL.Path = urlPath

			return next(c)
		}
	}
}
