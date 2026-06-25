package udp

import (
	"context"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/dns"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ipallowlist"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/redirect"
)

type UDPMiddleware struct {
	DNSMiddleware         *dns.DNS                 `cfg:"dns"`
	RedirectMiddleware    *redirect.Redirect       `cfg:"redirect"`
	IPAllowListMiddleware *ipallowlist.IPAllowList `cfg:"ip_allow_list"`
}

func (h *UDPMiddleware) getFirstFound(ctx context.Context, name string) ([]Handler, error) {
	switch {
	case h.DNSMiddleware != nil:
		m, err := h.DNSMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("dns middleware cannot create: %w", err)
		}

		return []Handler{m}, nil
	case h.RedirectMiddleware != nil:
		m, err := h.RedirectMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("redirect middleware cannot create: %w", err)
		}

		return []Handler{m}, nil
	case h.IPAllowListMiddleware != nil:
		m, err := h.IPAllowListMiddleware.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("ip allow list middleware cannot create: %w", err)
		}

		return []Handler{m}, nil
	}

	return nil, nil
}

func (h *UDPMiddleware) Set(ctx context.Context, name string) error {
	middleware, err := h.getFirstFound(ctx, name)
	if err != nil {
		return err
	}

	registry.GlobalReg.AddUDPMiddleware(name, middleware)

	return nil
}
