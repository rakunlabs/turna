// Package sessionlimit limits the UDP sessions, the client addresses seen
// recently, in total and per client IP.
package sessionlimit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

type SessionLimit struct {
	// Max sessions, 0 is unlimited.
	Max int `cfg:"max"`
	// MaxPerIP sessions of a client IP, 0 is unlimited.
	MaxPerIP int `cfg:"max_per_ip"`
	// Timeout ends a session without datagrams, default 60s.
	Timeout time.Duration `cfg:"timeout"`
}

type limiter struct {
	mu       sync.Mutex
	now      func() time.Time
	timeout  time.Duration
	max      int
	maxPerIP int
	sessions map[string]*session
	perIP    map[string]int
}

type session struct {
	ip   string
	last time.Time
}

func (l *limiter) allow(addr net.Addr) error {
	key := addr.String()
	ip := ipcheck.Host(addr)

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	if s, ok := l.sessions[key]; ok {
		if now.Sub(s.last) < l.timeout {
			s.last = now

			return nil
		}

		l.remove(key, s)
	}

	if l.max > 0 && len(l.sessions) >= l.max {
		l.expire(now)

		if len(l.sessions) >= l.max {
			return fmt.Errorf("%w: session limit %d reached", udpmw.ErrReject, l.max)
		}
	}

	if l.maxPerIP > 0 && l.perIP[ip] >= l.maxPerIP {
		l.expire(now)

		if l.perIP[ip] >= l.maxPerIP {
			return fmt.Errorf("%w: session limit %d reached for %s", udpmw.ErrReject, l.maxPerIP, ip)
		}
	}

	l.sessions[key] = &session{ip: ip, last: now}
	l.perIP[ip]++

	return nil
}

func (l *limiter) remove(key string, s *session) {
	delete(l.sessions, key)

	if l.perIP[s.ip]--; l.perIP[s.ip] <= 0 {
		delete(l.perIP, s.ip)
	}
}

func (l *limiter) expire(now time.Time) {
	for key, s := range l.sessions {
		if now.Sub(s.last) >= l.timeout {
			l.remove(key, s)
		}
	}
}

func (m *SessionLimit) Middleware(ctx context.Context, _ string) (udpmw.Handler, error) {
	if m.Max < 0 || m.MaxPerIP < 0 || m.Timeout < 0 {
		return nil, errors.New("max, max_per_ip and timeout must not be negative")
	}

	if m.Max == 0 && m.MaxPerIP == 0 {
		return nil, errors.New("max or max_per_ip is required")
	}

	timeout := m.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}

	l := &limiter{
		now:      time.Now,
		timeout:  timeout,
		max:      m.Max,
		maxPerIP: m.MaxPerIP,
		sessions: make(map[string]*session),
		perIP:    make(map[string]int),
	}

	go func() {
		ticker := time.NewTicker(min(timeout, 10*time.Second))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				l.mu.Lock()
				l.expire(l.now())
				l.mu.Unlock()
			}
		}
	}()

	return func(_ net.PacketConn, addr net.Addr, _ []byte) error {
		return l.allow(addr)
	}, nil
}
