package middlewares

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Decompress struct{}

func (m *Decompress) Middleware() echo.MiddlewareFunc {
	return middleware.Decompress()
}
