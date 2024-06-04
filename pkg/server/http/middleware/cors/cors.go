package cors

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Cors struct {
	AllowOrigins                             []string `cfg:"allow_origins"`
	AllowMethods                             []string `cfg:"allow_methods"`
	AllowHeaders                             []string `cfg:"allow_headers"`
	AllowCredentials                         bool     `cfg:"allow_credentials"`
	UnsafeWildcardOriginWithAllowCredentials bool     `cfg:"unsafe_wildcard_origin_with_allow_credentials"`
	ExposeHeaders                            []string `cfg:"expose_headers"`
	MaxAge                                   int      `cfg:"max_age"`
}

func (c *Cors) Middleware() echo.MiddlewareFunc {
	return middleware.CORSWithConfig(
		middleware.CORSConfig{
			AllowOrigins:                             c.AllowOrigins,
			AllowMethods:                             c.AllowMethods,
			AllowHeaders:                             c.AllowHeaders,
			AllowCredentials:                         c.AllowCredentials,
			UnsafeWildcardOriginWithAllowCredentials: c.UnsafeWildcardOriginWithAllowCredentials,
			ExposeHeaders:                            c.ExposeHeaders,
			MaxAge:                                   c.MaxAge,
		},
	)
}
