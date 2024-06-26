package role

import (
	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/pkg/authecho"
)

type Role struct {
	Roles   []string `cfg:"roles"`
	Methods []string `cfg:"methods"`
}

func (m *Role) Middleware() echo.MiddlewareFunc {
	return authecho.MiddlewareRole(
		authecho.WithRoles(m.Roles...),
		authecho.WithMethodsRole(m.Methods...),
	)
}
