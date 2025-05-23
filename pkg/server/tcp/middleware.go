package tcp

import (
	"context"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ipallowlist"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/socks5"
)

type TCPMiddleware struct {
	RedirectMiddleware    *redirect.Redirect       `cfg:"redirect"`
	Socks5Middleware      *socks5.Socks5           `cfg:"socks5"`
	IPAllowListMiddleware *ipallowlist.IPAllowList `cfg:"ip_allow_list"`
}

func (h *TCPMiddleware) getFirstFound(ctx context.Context, name string) ([]func(lconn *net.TCPConn) error, error) {
	switch {
	case h.RedirectMiddleware != nil:
		m, err := h.RedirectMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("redirect middleware cannot create: %w", err)
		}

		return []func(lconn *net.TCPConn) error{m}, nil
	case h.Socks5Middleware != nil:
		m, err := h.Socks5Middleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("socks5 middleware cannot create: %w", err)
		}

		return []func(lconn *net.TCPConn) error{m}, nil
	case h.IPAllowListMiddleware != nil:
		m, err := h.IPAllowListMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("ip allow list middleware cannot create: %w", err)
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
