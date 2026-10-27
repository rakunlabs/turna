// Package sniallowlist passes TLS connections only for allowed server names.
package sniallowlist

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/snimatch"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/snirouter"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// SNIAllowList reads the server name of the TLS ClientHello without
// consuming it and rejects connections whose name is not listed.
type SNIAllowList struct {
	// SNI names, "*.example.com" matches any subdomain.
	SNI []string `cfg:"sni"`
	// AllowMissing passes connections without SNI, e.g. clients connecting
	// by IP. Non-TLS connections are always rejected.
	AllowMissing bool `cfg:"allow_missing"`
	// Timeout to read the ClientHello, default is 5s.
	Timeout time.Duration `cfg:"timeout"`
}

const helloBuffer = 5 + 16384 + 2048

func (m *SNIAllowList) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if len(m.SNI) == 0 {
		return nil, errors.New("sni is required")
	}

	names := make([]string, 0, len(m.SNI))
	for _, n := range m.SNI {
		n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
		if n == "" {
			return nil, errors.New("sni must not be empty")
		}

		names = append(names, n)
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			_ = conn.SetReadDeadline(time.Now().Add(timeout))

			c, br := tcpmw.BufferedConn(conn, helloBuffer)
			sni, err := snirouter.PeekSNI(br)

			_ = conn.SetReadDeadline(time.Time{})

			if sni == "" {
				if err == nil && m.AllowMissing {
					return next(c)
				}

				return fmt.Errorf("%w: no tls server name", tcpmw.ErrReject)
			}

			sni = strings.ToLower(sni)
			for _, n := range names {
				if snimatch.Match(n, sni) {
					return next(c)
				}
			}

			return fmt.Errorf("%w: sni %q not allowed", tcpmw.ErrReject, sni)
		}
	}, nil
}
