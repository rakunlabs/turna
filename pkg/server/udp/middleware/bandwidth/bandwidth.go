// Package bandwidth limits the bytes per second exchanged with each UDP
// client. Datagrams over the limit are dropped.
package bandwidth

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/keylimit"
	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

// BandwidthLimit limits the transfer rate in bytes per second of each client
// IP. Upload is data from the client, download is replies to it.
type BandwidthLimit struct {
	Upload   int `cfg:"upload"`
	Download int `cfg:"download"`
	// Burst in bytes, default is the rate (1 second) and at least 65535 so a
	// full datagram fits.
	Burst int `cfg:"burst"`
}

func (m *BandwidthLimit) Middleware(ctx context.Context, _ string) (udpmw.Handler, error) {
	if m.Upload < 0 || m.Download < 0 || m.Burst < 0 {
		return nil, errors.New("upload, download and burst must not be negative")
	}

	if m.Upload == 0 && m.Download == 0 {
		return nil, errors.New("upload or download is required")
	}

	newLimiter := func(r int) *keylimit.Limiter {
		if r == 0 {
			return nil
		}

		burst := m.Burst
		if burst == 0 {
			burst = max(r, 65535)
		}

		return keylimit.New(ctx, float64(r), burst)
	}

	upload := newLimiter(m.Upload)
	download := newLimiter(m.Download)

	return func(conn net.PacketConn, addr net.Addr, data []byte) error {
		ip := ipcheck.Host(addr)

		if upload != nil && !upload.AllowN(ip, len(data)) {
			return fmt.Errorf("%w: upload bandwidth exceeded for %s", udpmw.ErrReject, ip)
		}

		if download != nil {
			udpmw.Wrap(conn, func(pc net.PacketConn) net.PacketConn {
				return &Conn{PacketConn: pc, limit: download}
			})
		}

		return nil
	}, nil
}

// Conn drops replies over the download limit of their client.
type Conn struct {
	net.PacketConn

	limit *keylimit.Limiter
}

func (c *Conn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if !c.limit.AllowN(ipcheck.Host(addr), len(b)) {
		// a dropped datagram is not an error for UDP
		return len(b), nil
	}

	return c.PacketConn.WriteTo(b, addr)
}
