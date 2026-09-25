package proxyprotocol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/proxyproto"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// ProxyProtocol reads an incoming PROXY protocol v1/v2 header and replaces
// the remote address of the connection with the original client address, so
// the next middlewares (ip_allow_list, rate_limit, log...) see the real client.
type ProxyProtocol struct {
	// TrustedIPs are IPs or CIDRs allowed to send a PROXY header. Required.
	TrustedIPs []string `cfg:"trusted_ips"`
	// Optional accepts connections without a header from trusted IPs.
	Optional bool `cfg:"optional"`
	// Timeout to read the header, default is 5s.
	Timeout time.Duration `cfg:"timeout"`
}

func (m *ProxyProtocol) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if len(m.TrustedIPs) == 0 {
		return nil, errors.New("trusted_ips is required")
	}

	checker, err := ipcheck.NewChecker(m.TrustedIPs)
	if err != nil {
		return nil, err
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			if checker.IsAuthorized(conn.RemoteAddr().String()) != nil {
				// untrusted peers never send a header we would honour
				return next(conn)
			}

			_ = conn.SetReadDeadline(time.Now().Add(timeout))

			c, br := tcpmw.BufferedConn(conn, 4096)

			h, err := proxyproto.Read(br)

			_ = conn.SetReadDeadline(time.Time{})

			switch {
			case err == nil:
			case errors.Is(err, proxyproto.ErrNotProxy) && m.Optional:
				return next(c)
			case proxyproto.IsNoInfo(err):
				return next(c)
			default:
				return fmt.Errorf("%w: proxy protocol: %w", tcpmw.ErrReject, err)
			}

			if !h.Local {
				c.Remote = h.Source
				c.LocalAddrV = h.Destination
			}

			return next(c)
		}
	}, nil
}
