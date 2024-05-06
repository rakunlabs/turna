package tcp

import (
	"context"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middlewares"
)

type TCPMiddleware struct {
	RedirectMiddleware *middlewares.Redirect `cfg:"redirect"`
}

func (h *TCPMiddleware) getFirstFound(ctx context.Context, name string) ([]func(lconn *net.TCPConn) error, error) {
	switch {
	case h.RedirectMiddleware != nil:
		m, err := h.RedirectMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("redirect middleware cannot create: %w", err)
		}

		return []func(lconn *net.TCPConn) error{m}, nil
	}

	return nil, nil
}

func (h *TCPMiddleware) Set(ctx context.Context, name string) error {
	middleware, err := h.getFirstFound(ctx, name)
	if err != nil {
		return err
	}

	registry.GlobalReg.AddTcpMiddleware(name, middleware)

	return nil
}
