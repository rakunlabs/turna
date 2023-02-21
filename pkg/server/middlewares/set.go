package middlewares

import (
	"github.com/labstack/echo/v4"
)

// Set to set flag.
//
// Usable for other middlewares.
type Set struct {
	Values []string `cfg:"values"`
}

func (s *Set) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for _, v := range s.Values {
				c.Set(v, true)
			}

			return next(c)
		}
	}
}
