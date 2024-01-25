package user

import (
	"context"

	"github.com/labstack/echo/v4"
)

type User struct {
}

func (m *User) Middleware(ctx context.Context, name string) (echo.MiddlewareFunc, error) {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			return nil
		}
	}, nil
}
