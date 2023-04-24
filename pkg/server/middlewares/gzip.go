package middlewares

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Gzip struct{}

func (m *Gzip) Middleware() echo.MiddlewareFunc {
	return middleware.Gzip()
}
