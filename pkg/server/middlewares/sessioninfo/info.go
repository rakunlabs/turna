package sessioninfo

import (
	"github.com/labstack/echo/v4"
)

type Info struct {
	Information       Information `cfg:"information"`
	SessionMiddleware string      `cfg:"session_middleware"`
}

func (m *Info) Middleware() (echo.MiddlewareFunc, error) {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return m.Info(c)
		}
	}, nil
}
