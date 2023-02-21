package http

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/registry"
)

var ServerInfo = "Turna"

type Router struct {
	Path        string   `cfg:"path"`
	Middlewares []string `cfg:"middlewares"`
}

func (r *Router) Check() error {
	return nil
}

func (r *Router) Set(name string) error {
	e := registry.GlobalReg.Echo

	middlewares := make([]echo.MiddlewareFunc, 0, len(r.Middlewares)+2)
	middlewares = append(middlewares, PreMiddleware)

	for _, middlewareName := range r.Middlewares {
		middleware, err := registry.GlobalReg.GetHttpMiddleware(middlewareName)
		if err != nil {
			return err
		}

		middlewares = append(middlewares, middleware...)
	}

	e.Group(r.Path, middlewares...).Use(AfterMiddleware)
	// middlewares = append(middlewares, NewProxyHandler(r.Service))
	// e.Use(middlewares...)

	return nil
}

var AfterMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().WriteHeader(http.StatusNoContent)
		_, _ = c.Response().Writer.Write(nil)

		return nil
	}
}

var PreMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("Server", ServerInfo)
		return next(c)
	}
}
