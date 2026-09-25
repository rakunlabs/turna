package connlimit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// ConnLimit limits concurrent connections, in total and per client IP.
type ConnLimit struct {
	// Max concurrent connections, 0 is unlimited.
	Max int `cfg:"max"`
	// MaxPerIP concurrent connections of a client IP, 0 is unlimited.
	MaxPerIP int `cfg:"max_per_ip"`
}

func (m *ConnLimit) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if m.Max < 0 || m.MaxPerIP < 0 {
		return nil, errors.New("max and max_per_ip must not be negative")
	}

	if m.Max == 0 && m.MaxPerIP == 0 {
		return nil, errors.New("max or max_per_ip is required")
	}

	var (
		mu    sync.Mutex
		total int
		perIP = map[string]int{}
	)

	acquire := func(ip string) error {
		mu.Lock()
		defer mu.Unlock()

		if m.Max > 0 && total >= m.Max {
			return fmt.Errorf("%w: connection limit %d reached", tcpmw.ErrReject, m.Max)
		}

		if m.MaxPerIP > 0 && perIP[ip] >= m.MaxPerIP {
			return fmt.Errorf("%w: connection limit %d reached for %s", tcpmw.ErrReject, m.MaxPerIP, ip)
		}

		total++
		perIP[ip]++

		return nil
	}

	release := func(ip string) {
		mu.Lock()
		defer mu.Unlock()

		total--
		if perIP[ip]--; perIP[ip] <= 0 {
			delete(perIP, ip)
		}
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			ip := ipcheck.Host(conn.RemoteAddr())

			if err := acquire(ip); err != nil {
				return err
			}
			defer release(ip)

			return next(conn)
		}
	}, nil
}
