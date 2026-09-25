package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/keylimit"
	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

// RateLimit limits datagrams per second of each client IP with a token
// bucket; extra datagrams are dropped.
type RateLimit struct {
	// Rate is the allowed datagrams per second.
	Rate float64 `cfg:"rate"`
	// Burst is the bucket size, default is max(1, rate).
	Burst int `cfg:"burst"`
}

func (m *RateLimit) Middleware(ctx context.Context, _ string) (udpmw.Handler, error) {
	if m.Rate <= 0 {
		return nil, errors.New("rate must be greater than 0")
	}

	limiter := keylimit.New(ctx, m.Rate, m.Burst)

	return func(_ net.PacketConn, addr net.Addr, _ []byte) error {
		ip := ipcheck.Host(addr)
		if !limiter.Allow(ip) {
			return fmt.Errorf("%w: rate limit exceeded for %s", udpmw.ErrReject, ip)
		}

		return nil
	}, nil
}
