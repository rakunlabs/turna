package loadbalancer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rakunlabs/turna/pkg/server/netlb"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// LoadBalancer proxies connections to one of the upstream servers.
type LoadBalancer struct {
	Servers []netlb.Server `cfg:"servers"`
	// Strategy is round_robin (default), random or least_conn.
	Strategy string `cfg:"strategy"`
	// Retry dials other upstreams when dialing fails, default is 0.
	Retry int `cfg:"retry"`

	DialTimeout  time.Duration `cfg:"dial_timeout"`
	DisableNagle bool          `cfg:"disable_nagle"`

	// ProxyProtocol sends a PROXY protocol header to the upstream.
	ProxyProtocol        bool `cfg:"proxy_protocol"`
	ProxyProtocolVersion int  `cfg:"proxy_protocol_version"`

	// HealthCheck actively dials every upstream.
	HealthCheck *netlb.HealthCheck `cfg:"health_check"`
	// PassiveHealthCheck ejects upstreams with dial failures.
	PassiveHealthCheck *netlb.PassiveHealthCheck `cfg:"passive_health_check"`
}

func (m *LoadBalancer) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	if m.Retry < 0 {
		return nil, errors.New("retry must not be negative")
	}

	f, err := (&redirect.Redirect{
		DialTimeout:          m.DialTimeout,
		DisableNagle:         m.DisableNagle,
		ProxyProtocol:        m.ProxyProtocol,
		ProxyProtocolVersion: m.ProxyProtocolVersion,
	}).Forwarder()
	if err != nil {
		return nil, err
	}

	if f.DialTimeout <= 0 {
		f.DialTimeout = 10 * time.Second
	}

	for _, s := range m.Servers {
		if err := redirect.ValidateNetwork("tcp", s.Address); err != nil {
			return nil, err
		}
	}

	b, err := netlb.New(m.Strategy, m.Servers, m.PassiveHealthCheck)
	if err != nil {
		return nil, err
	}

	if m.HealthCheck != nil {
		b.StartHealthCheck(ctx, *m.HealthCheck, func(ctx context.Context, address string) error {
			var d net.Dialer

			c, err := d.DialContext(ctx, "tcp", address)
			if err != nil {
				return err
			}

			return c.Close()
		})
	}

	return func(_ tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			var (
				tried   []*netlb.Target
				lastErr error
			)

			for range m.Retry + 1 {
				t, err := b.Next(tried)
				if err != nil {
					break
				}

				tried = append(tried, t)

				release := b.Acquire(t)

				rconn, err := f.Dial(ctx, conn, t.Address)
				if err != nil {
					release()
					b.ReportFailure(t)

					lastErr = err

					continue
				}

				b.ReportSuccess(t)

				redirect.Forward(conn, rconn)
				release()

				return nil
			}

			if lastErr == nil {
				lastErr = netlb.ErrNoTarget
			}

			return fmt.Errorf("load balancer: %w", lastErr)
		}
	}, nil
}
