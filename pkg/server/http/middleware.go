package http

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/turna/pkg/server/middlewares"
	"github.com/worldline-go/turna/pkg/server/registry"
)

type HTTPMiddleware struct {
	AddPrefixMiddleware   *middlewares.AddPrefix   `cfg:"add_prefix"`
	AuthMiddleware        *middlewares.Auth        `cfg:"auth"`
	HelloMiddleware       *middlewares.Hello       `cfg:"hello"`
	InfoMiddleware        *middlewares.Info        `cfg:"info"`
	SetMiddleware         *middlewares.Set         `cfg:"set"`
	StripPrefixMiddleware *middlewares.StripPrefix `cfg:"strip_prefix"`
	RoleMiddleware        *middlewares.Role        `cfg:"role"`
	ScopeMiddleware       *middlewares.Scope       `cfg:"scope"`
	ServiceMiddleware     *middlewares.Service     `cfg:"service"`
	FolderMiddleware      *middlewares.Folder      `cfg:"folder"`
	BasicAuthMiddleware   *middlewares.BasicAuth   `cfg:"basic_auth"`
	CorsMiddleware        *middlewares.Cors        `cfg:"cors"`
	HeadersMiddleware     *middlewares.Headers     `cfg:"headers"`
	BlockMiddleware       *middlewares.Block       `cfg:"block"`
}

func (h *HTTPMiddleware) getFirstFound(ctx context.Context, name string) ([]echo.MiddlewareFunc, error) {
	if h.AddPrefixMiddleware != nil {
		return []echo.MiddlewareFunc{h.AddPrefixMiddleware.Middleware()}, nil
	}
	if h.AuthMiddleware != nil {
		return h.AuthMiddleware.Middleware(ctx, name)
	}
	if h.HelloMiddleware != nil {
		return []echo.MiddlewareFunc{h.HelloMiddleware.Middleware()}, nil
	}
	if h.InfoMiddleware != nil {
		return []echo.MiddlewareFunc{h.InfoMiddleware.Middleware()}, nil
	}
	if h.SetMiddleware != nil {
		return []echo.MiddlewareFunc{h.SetMiddleware.Middleware()}, nil
	}
	if h.StripPrefixMiddleware != nil {
		return []echo.MiddlewareFunc{h.StripPrefixMiddleware.Middleware()}, nil
	}
	if h.RoleMiddleware != nil {
		return []echo.MiddlewareFunc{h.RoleMiddleware.Middleware()}, nil
	}
	if h.ScopeMiddleware != nil {
		return []echo.MiddlewareFunc{h.ScopeMiddleware.Middleware()}, nil
	}
	if h.ServiceMiddleware != nil {
		return h.ServiceMiddleware.Middleware()
	}
	if h.FolderMiddleware != nil {
		return []echo.MiddlewareFunc{h.FolderMiddleware.Middleware()}, nil
	}
	if h.BasicAuthMiddleware != nil {
		return h.BasicAuthMiddleware.Middleware(name)
	}
	if h.CorsMiddleware != nil {
		return []echo.MiddlewareFunc{h.CorsMiddleware.Middleware()}, nil
	}
	if h.HeadersMiddleware != nil {
		return []echo.MiddlewareFunc{h.HeadersMiddleware.Middleware()}, nil
	}
	if h.BlockMiddleware != nil {
		return []echo.MiddlewareFunc{h.BlockMiddleware.Middleware()}, nil
	}

	return nil, nil
}

func (h *HTTPMiddleware) Set(ctx context.Context, name string) error {
	middleware, err := h.getFirstFound(ctx, name)
	if err != nil {
		return err
	}

	registry.GlobalReg.AddHttpMiddleware(name, middleware)

	return nil
}
