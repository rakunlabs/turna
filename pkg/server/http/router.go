package http

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/registry"
)

var ServerInfo = "Turna"

type Router struct {
	Host        string    `cfg:"host"`
	Path        string    `cfg:"path"`
	Middlewares []string  `cfg:"middlewares"`
	TLS         *struct{} `cfg:"tls"`
	EntryPoints []string  `cfg:"entrypoints"`
}

func (r *Router) Check() error {
	return nil
}

func (r *Router) Set(_ string, ruleRouter *RuleRouter) error {
	entrypoints := r.EntryPoints
	if len(entrypoints) == 0 {
		entrypoints = registry.GlobalReg.GetListenerNamesList()
	}

	for _, entrypoint := range entrypoints {
		e := ruleRouter.GetEcho(RuleSelection{
			Host:       r.Host,
			Entrypoint: entrypoint,
		})

		if e == nil {
			return fmt.Errorf("entrypoint %s, host %s, echo does not exist", entrypoint, r.Host)
		}

		middlewares := make([]echo.MiddlewareFunc, 0, len(r.Middlewares)+2)
		middlewares = append(middlewares, PreMiddleware)

		for _, middlewareName := range r.Middlewares {
			middleware, err := registry.GlobalReg.GetHttpMiddleware(middlewareName)
			if err != nil {
				return err
			}

			middlewares = append(middlewares, middleware...)
		}

		e.Group(r.Path, middlewares...).Use(PostMiddleware)
		// middlewares = append(middlewares, NewProxyHandler(r.Service))
		// e.Use(middlewares...)
	}

	return nil
}

var PostMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc {
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
