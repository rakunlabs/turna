package middlewares

import (
	"strings"

	"github.com/labstack/echo/v4"
)

type Block struct {
	Methods []string `cfg:"methods"`
}

func (b *Block) Middleware() echo.MiddlewareFunc {
	methodsSet := make(map[string]struct{}, len(b.Methods))
	for _, m := range b.Methods {
		methodsSet[strings.ToUpper(m)] = struct{}{}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if _, ok := methodsSet[c.Request().Method]; ok {
				return echo.ErrMethodNotAllowed
			}

			return next(c)
		}
	}
}
