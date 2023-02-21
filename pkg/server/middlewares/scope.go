package middlewares

import (
	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/middlewares/authecho"
)

type Scope struct {
	Scopes  []string `cfg:"scopes"`
	Methods []string `cfg:"methods"`
}

func (m *Scope) Middleware() echo.MiddlewareFunc {
	return authecho.MiddlewareScope(
		authecho.WithScopes(m.Scopes...),
		authecho.WithMethodsScope(m.Methods...),
	)
}
