package middlewares

import (
	"net/url"

	"github.com/labstack/echo/v4"
)

type AddPrefix struct {
	Prefix string `cfg:"prefix"`
}

func (a *AddPrefix) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			urlPath, err := url.JoinPath(a.Prefix + c.Request().URL.Path)
			if err != nil {
				return err
			}

			c.Request().URL.Path = urlPath

			return next(c)
		}
	}
}
