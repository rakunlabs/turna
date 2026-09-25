package tcp

import (
	"context"
	"fmt"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/bandwidth"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/connlimit"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/httpconnect"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/idletimeout"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ipallowlist"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ipdenylist"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/loadbalancer"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/log"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/proxyprotocol"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/ratelimit"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/snirouter"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/socks5"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/telemetry"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/tlsterminate"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type TCPMiddleware struct {
	RedirectMiddleware       *redirect.Redirect           `cfg:"redirect"`
	Socks5Middleware         *socks5.Socks5               `cfg:"socks5"`
	IPAllowListMiddleware    *ipallowlist.IPAllowList     `cfg:"ip_allow_list"`
	IPDenyListMiddleware     *ipdenylist.IPDenyList       `cfg:"ip_deny_list"`
	ConnLimitMiddleware      *connlimit.ConnLimit         `cfg:"conn_limit"`
	RateLimitMiddleware      *ratelimit.RateLimit         `cfg:"rate_limit"`
	IdleTimeoutMiddleware    *idletimeout.IdleTimeout     `cfg:"idle_timeout"`
	BandwidthLimitMiddleware *bandwidth.BandwidthLimit    `cfg:"bandwidth_limit"`
	ProxyProtocolMiddleware  *proxyprotocol.ProxyProtocol `cfg:"proxy_protocol"`
	TLSTerminateMiddleware   *tlsterminate.TLSTerminate   `cfg:"tls_terminate"`
	SNIRouterMiddleware      *snirouter.SNIRouter         `cfg:"sni_router"`
	LoadBalancerMiddleware   *loadbalancer.LoadBalancer   `cfg:"load_balancer"`
	LogMiddleware            *log.Log                     `cfg:"log"`
	TelemetryMiddleware      *telemetry.Telemetry         `cfg:"telemetry"`
	HTTPConnectMiddleware    *httpconnect.HTTPConnect     `cfg:"http_connect"`
}

type middlewareFactory interface {
	Middleware(ctx context.Context, name string) (tcpmw.Middleware, error)
}

func (h *TCPMiddleware) factories() []struct {
	name    string
	factory middlewareFactory
	set     bool
} {
	return []struct {
		name    string
		factory middlewareFactory
		set     bool
	}{
		{"redirect", h.RedirectMiddleware, h.RedirectMiddleware != nil},
		{"socks5", h.Socks5Middleware, h.Socks5Middleware != nil},
		{"ip allow list", h.IPAllowListMiddleware, h.IPAllowListMiddleware != nil},
		{"ip deny list", h.IPDenyListMiddleware, h.IPDenyListMiddleware != nil},
		{"conn limit", h.ConnLimitMiddleware, h.ConnLimitMiddleware != nil},
		{"rate limit", h.RateLimitMiddleware, h.RateLimitMiddleware != nil},
		{"idle timeout", h.IdleTimeoutMiddleware, h.IdleTimeoutMiddleware != nil},
		{"bandwidth limit", h.BandwidthLimitMiddleware, h.BandwidthLimitMiddleware != nil},
		{"proxy protocol", h.ProxyProtocolMiddleware, h.ProxyProtocolMiddleware != nil},
		{"tls terminate", h.TLSTerminateMiddleware, h.TLSTerminateMiddleware != nil},
		{"sni router", h.SNIRouterMiddleware, h.SNIRouterMiddleware != nil},
		{"load balancer", h.LoadBalancerMiddleware, h.LoadBalancerMiddleware != nil},
		{"log", h.LogMiddleware, h.LogMiddleware != nil},
		{"telemetry", h.TelemetryMiddleware, h.TelemetryMiddleware != nil},
		{"http connect", h.HTTPConnectMiddleware, h.HTTPConnectMiddleware != nil},
	}
}

func (h *TCPMiddleware) getFirstFound(ctx context.Context, name string) ([]tcpmw.Middleware, error) {
	for _, f := range h.factories() {
		if !f.set {
			continue
		}

		m, err := f.factory.Middleware(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("%s middleware cannot create: %w", f.name, err)
		}

		return []tcpmw.Middleware{m}, nil
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
