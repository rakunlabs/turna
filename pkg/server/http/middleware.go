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
	InjectMiddleware      *middlewares.Inject      `cfg:"inject"`
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
	RegexPathMiddleware   *middlewares.RegexPath   `cfg:"regex_path"`
	GzipMiddleware        *middlewares.Gzip        `cfg:"gzip"`
	DecompressMiddleware  *middlewares.Decompress  `cfg:"decompress"`
	LogMiddleware         *middlewares.Log         `cfg:"log"`
}

func (h *HTTPMiddleware) getFirstFound(ctx context.Context, name string) ([]echo.MiddlewareFunc, error) {
	if h.AddPrefixMiddleware != nil {
		return []echo.MiddlewareFunc{h.AddPrefixMiddleware.Middleware()}, nil
	}
	if h.AuthMiddleware != nil {
		return h.AuthMiddleware.Middleware(ctx, name)
	}
	if h.InjectMiddleware != nil {
		return []echo.MiddlewareFunc{h.InjectMiddleware.Middleware()}, nil
	}
	if h.HelloMiddleware != nil {
		return h.HelloMiddleware.Middleware()
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
		return h.FolderMiddleware.Middleware()
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
	if h.RegexPathMiddleware != nil {
		return h.RegexPathMiddleware.Middleware()
	}
	if h.GzipMiddleware != nil {
		return []echo.MiddlewareFunc{h.GzipMiddleware.Middleware()}, nil
	}
	if h.DecompressMiddleware != nil {
		return []echo.MiddlewareFunc{h.DecompressMiddleware.Middleware()}, nil
	}
	if h.LogMiddleware != nil {
		return h.LogMiddleware.Middleware()
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
