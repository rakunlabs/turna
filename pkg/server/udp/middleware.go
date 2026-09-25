package udp

import (
	"context"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/dns"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ipallowlist"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ipdenylist"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/log"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/mirror"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/ratelimit"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/udp/middleware/telemetry"
)

type UDPMiddleware struct {
	DNSMiddleware          *dns.DNS                   `cfg:"dns"`
	RedirectMiddleware     *redirect.Redirect         `cfg:"redirect"`
	IPAllowListMiddleware  *ipallowlist.IPAllowList   `cfg:"ip_allow_list"`
	IPDenyListMiddleware   *ipdenylist.IPDenyList     `cfg:"ip_deny_list"`
	RateLimitMiddleware    *ratelimit.RateLimit       `cfg:"rate_limit"`
	LoadBalancerMiddleware *loadbalancer.LoadBalancer `cfg:"load_balancer"`
	MirrorMiddleware       *mirror.Mirror             `cfg:"mirror"`
	LogMiddleware          *log.Log                   `cfg:"log"`
	TelemetryMiddleware    *telemetry.Telemetry       `cfg:"telemetry"`
}

type middlewareFactory interface {
	Middleware(ctx context.Context, name string) (Handler, error)
}

func (h *UDPMiddleware) getFirstFound(ctx context.Context, name string) ([]Handler, error) {
	for _, f := range []struct {
		name    string
		factory middlewareFactory
		set     bool
	}{
		{"dns", h.DNSMiddleware, h.DNSMiddleware != nil},
		{"redirect", h.RedirectMiddleware, h.RedirectMiddleware != nil},
		{"ip allow list", h.IPAllowListMiddleware, h.IPAllowListMiddleware != nil},
		{"ip deny list", h.IPDenyListMiddleware, h.IPDenyListMiddleware != nil},
		{"rate limit", h.RateLimitMiddleware, h.RateLimitMiddleware != nil},
		{"load balancer", h.LoadBalancerMiddleware, h.LoadBalancerMiddleware != nil},
		{"mirror", h.MirrorMiddleware, h.MirrorMiddleware != nil},
		{"log", h.LogMiddleware, h.LogMiddleware != nil},
		{"telemetry", h.TelemetryMiddleware, h.TelemetryMiddleware != nil},
	} {
		if !f.set {
			continue
		}

		m, err := f.factory.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("%s middleware cannot create: %w", f.name, err)
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
