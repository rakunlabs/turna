package loadbalancer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/netlb"
)

// LoadBalancer forwards datagrams to upstream servers. Every client address
// gets a session bound to one upstream, so all datagrams of a client go to
// the same upstream and every reply of the upstream is sent back.
type LoadBalancer struct {
	Servers []netlb.Server `cfg:"servers"`
	// Strategy is round_robin (default), random or least_conn (sessions).
	Strategy string `cfg:"strategy"`
	Network  string `cfg:"network"`
	// SessionTimeout closes sessions without traffic, default is 30s.
	SessionTimeout time.Duration `cfg:"session_timeout"`
	// MaxSessions bounds the open sessions, default is 10000.
	MaxSessions int `cfg:"max_sessions"`

	// HealthCheck sends Payload to every upstream and expects a reply.
	HealthCheck *HealthCheck `cfg:"health_check"`
	// PassiveHealthCheck ejects upstreams with send errors (e.g. ICMP port
	// unreachable).
	PassiveHealthCheck *netlb.PassiveHealthCheck `cfg:"passive_health_check"`
}

type HealthCheck struct {
	Interval           time.Duration `cfg:"interval"`
	Timeout            time.Duration `cfg:"timeout"`
	HealthyThreshold   int           `cfg:"healthy_threshold"`
	UnhealthyThreshold int           `cfg:"unhealthy_threshold"`

	// Payload sent to the upstream, required.
	Payload string `cfg:"payload"`
	// Expect is a substring the reply must contain, empty accepts any reply.
	Expect string `cfg:"expect"`
}

type session struct {
	target  *netlb.Target
	upConn  net.Conn
	release func()

	mu       sync.Mutex
	lastSeen time.Time
}

func (s *session) touch() {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

func (s *session) idle(now time.Time, timeout time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return now.Sub(s.lastSeen) > timeout
}

type balancer struct {
	ctx      context.Context
	network  string
	b        *netlb.Balancer
	timeout  time.Duration
	max      int
	sessions map[string]*session
	mu       sync.Mutex
}

func (m *LoadBalancer) Middleware(ctx context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	network := m.Network
	if network == "" {
		network = "udp"
	}

	if !strings.HasPrefix(network, "udp") {
		return nil, fmt.Errorf("unsupported network %s, only udp is supported", network)
	}

	for _, s := range m.Servers {
		if _, err := net.ResolveUDPAddr(network, s.Address); err != nil {
			return nil, fmt.Errorf("address cannot resolve %s: %w", s.Address, err)
		}
	}

	b, err := netlb.New(m.Strategy, m.Servers, m.PassiveHealthCheck)
	if err != nil {
		return nil, err
	}

	lb := &balancer{
		ctx:      ctx,
		network:  network,
		b:        b,
		timeout:  m.SessionTimeout,
		max:      m.MaxSessions,
		sessions: map[string]*session{},
	}

	if lb.timeout <= 0 {
		lb.timeout = 30 * time.Second
	}

	if lb.max <= 0 {
		lb.max = 10000
	}

	if hc := m.HealthCheck; hc != nil {
		if hc.Payload == "" {
			return nil, errors.New("health_check.payload is required")
		}

		cfg := netlb.HealthCheck{
			Interval:           hc.Interval,
			Timeout:            hc.Timeout,
			HealthyThreshold:   hc.HealthyThreshold,
			UnhealthyThreshold: hc.UnhealthyThreshold,
		}

		b.StartHealthCheck(ctx, cfg, func(ctx context.Context, address string) error {
			return probe(ctx, network, address, []byte(hc.Payload), []byte(hc.Expect))
		})
	}

	go lb.cleanup()

	return lb.handle, nil
}

func (lb *balancer) handle(conn net.PacketConn, addr net.Addr, data []byte) error {
	s, err := lb.session(conn, addr)
	if err != nil {
		return err
	}

	s.touch()

	if _, err := s.upConn.Write(data); err != nil {
		lb.b.ReportFailure(s.target)
		lb.close(addr.String(), s)

		return fmt.Errorf("write to %s: %w", s.target.Address, err)
	}

	return nil
}

func (lb *balancer) session(conn net.PacketConn, addr net.Addr) (*session, error) {
	key := addr.String()

	lb.mu.Lock()
	defer lb.mu.Unlock()

	if s, ok := lb.sessions[key]; ok {
		return s, nil
	}

	if len(lb.sessions) >= lb.max {
		return nil, fmt.Errorf("max sessions %d reached", lb.max)
	}

	var (
		tried   []*netlb.Target
		lastErr error
	)

	for range len(lb.b.Targets()) {
		t, err := lb.b.Next(tried)
		if err != nil {
			break
		}

		tried = append(tried, t)

		var d net.Dialer

		upConn, err := d.DialContext(lb.ctx, lb.network, t.Address)
		if err != nil {
			lb.b.ReportFailure(t)
			lastErr = err

			continue
		}

		s := &session{target: t, upConn: upConn, release: lb.b.Acquire(t), lastSeen: time.Now()}
		lb.sessions[key] = s

		go lb.relay(conn, addr, key, s)

		return s, nil
	}

	if lastErr == nil {
		lastErr = netlb.ErrNoTarget
	}

	return nil, fmt.Errorf("load balancer: %w", lastErr)
}

// relay sends upstream replies back to the client until the session closes.
func (lb *balancer) relay(conn net.PacketConn, addr net.Addr, key string, s *session) {
	buf := make([]byte, 65535)

	for {
		n, err := s.upConn.Read(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				// e.g. connection refused from ICMP port unreachable
				lb.b.ReportFailure(s.target)
				slog.Debug("udp upstream read failed", "upstream", s.target.Address, "err", err.Error())
			}

			lb.close(key, s)

			return
		}

		s.touch()
		lb.b.ReportSuccess(s.target)

		if _, err := conn.WriteTo(buf[:n], addr); err != nil {
			slog.Debug("udp reply write failed", "remote", key, "err", err.Error())
		}
	}
}

func (lb *balancer) close(key string, s *session) {
	lb.mu.Lock()
	if cur, ok := lb.sessions[key]; ok && cur == s {
		delete(lb.sessions, key)
	}
	lb.mu.Unlock()

	s.upConn.Close()
	s.release()
}

func (lb *balancer) cleanup() {
	interval := min(lb.timeout, 10*time.Second)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-lb.ctx.Done():
			lb.mu.Lock()
			sessions := lb.sessions
			lb.sessions = map[string]*session{}
			lb.mu.Unlock()

			for _, s := range sessions {
				s.upConn.Close()
				s.release()
			}

			return
		case now := <-ticker.C:
			var expired []*session

			var keys []string

			lb.mu.Lock()
			for k, s := range lb.sessions {
				if s.idle(now, lb.timeout) {
					expired = append(expired, s)
					keys = append(keys, k)
				}
			}
			lb.mu.Unlock()

			for i, s := range expired {
				lb.close(keys[i], s)
			}
		}
	}
}

func probe(ctx context.Context, network, address string, payload, expect []byte) error {
	var d net.Dialer

	c, err := d.DialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer c.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(deadline)
	}

	if _, err := c.Write(payload); err != nil {
		return err
	}

	buf := make([]byte, 65535)

	n, err := c.Read(buf)
	if err != nil {
		return err
	}

	if len(expect) > 0 && !bytes.Contains(buf[:n], expect) {
		return errors.New("unexpected health check reply")
	}

	return nil
}
