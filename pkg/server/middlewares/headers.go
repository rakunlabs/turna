package middlewares

import "github.com/labstack/echo/v4"

// Headers is a middleware that allows to add custom headers to the request and response.
//
// Delete the header, set empty values.
type Headers struct {
	CustomRequestHeaders  map[string]string `cfg:"custom_request_headers"`
	CustomResponseHeaders map[string]string `cfg:"custom_response_headers"`
}

func (h *Headers) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for k, v := range h.CustomRequestHeaders {
				if v == "" {
					c.Request().Header.Del(k)
					continue
				}

				c.Request().Header.Set(k, v)
			}
			for k, v := range h.CustomResponseHeaders {
				if v == "" {
					c.Response().Header().Del(k)
					continue
				}

				c.Response().Header().Set(k, v)
			}

			return next(c)
		}
	}
}
