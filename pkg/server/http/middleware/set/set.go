package set

import (
	"github.com/labstack/echo/v4"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
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
			turna, _ := c.Get("turna").(*tcontext.Turna)

			for _, v := range s.Values {
				c.Set(v, true)
				if turna != nil {
					turna.Set(v, true)
				}
			}

			for k, v := range s.Map {
				c.Set(k, v)
				if turna != nil {
					turna.Set(k, v)
				}
			}

			return next(c)
		}
	}
}
