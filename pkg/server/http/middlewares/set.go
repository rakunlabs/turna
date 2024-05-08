package middlewares

import (
	"github.com/labstack/echo/v4"
)

// Set to set flag.
//
// Usable for other middlewares.
type Set struct {
	Values []string               `cfg:"values"`
	Map    map[string]interface{} `cfg:"map"`
}

func (s *Set) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for _, v := range s.Values {
				c.Set(v, true)
			}

			for k, v := range s.Map {
				c.Set(k, v)
			}

			return next(c)
		}
	}
}
