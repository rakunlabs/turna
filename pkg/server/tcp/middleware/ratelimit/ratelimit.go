package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/keylimit"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// RateLimit limits new connections per second of each client IP with a
// token bucket.
type RateLimit struct {
	// Rate is the allowed new connections per second.
	Rate float64 `cfg:"rate"`
	// Burst is the bucket size, default is max(1, rate).
	Burst int `cfg:"burst"`
}

func (m *RateLimit) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	if m.Rate <= 0 {
		return nil, errors.New("rate must be greater than 0")
	}

	limiter := keylimit.New(ctx, m.Rate, m.Burst)

	return tcpmw.Filter(func(conn net.Conn) error {
		ip := ipcheck.Host(conn.RemoteAddr())
		if !limiter.Allow(ip) {
			return fmt.Errorf("%w: rate limit exceeded for %s", tcpmw.ErrReject, ip)
		}

		return nil
	}), nil
}
