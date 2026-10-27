package sessionlimit

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func addr(ip string, port int) net.Addr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
}

func newLimiter(maxTotal, perIP int) (*limiter, *clock) {
	c := &clock{t: time.Unix(1_000_000, 0)}

	return &limiter{
		now:      c.now,
		timeout:  time.Minute,
		max:      maxTotal,
		maxPerIP: perIP,
		sessions: map[string]*session{},
		perIP:    map[string]int{},
	}, c
}

func TestPerIP(t *testing.T) {
	l, c := newLimiter(0, 2)

	for port := range 2 {
		if err := l.allow(addr("10.0.0.1", port)); err != nil {
			t.Fatal(err)
		}
	}

	if err := l.allow(addr("10.0.0.1", 9)); !errors.Is(err, udpmw.ErrReject) {
		t.Fatalf("third session: %v", err)
	}

	// existing sessions keep working
	if err := l.allow(addr("10.0.0.1", 0)); err != nil {
		t.Fatal(err)
	}

	if err := l.allow(addr("10.0.0.2", 0)); err != nil {
		t.Fatal("other ip limited")
	}

	c.t = c.t.Add(2 * time.Minute)

	if err := l.allow(addr("10.0.0.1", 9)); err != nil {
		t.Fatalf("expired sessions still counted: %v", err)
	}
}

func TestTotal(t *testing.T) {
	l, c := newLimiter(2, 0)

	_ = l.allow(addr("10.0.0.1", 1))
	_ = l.allow(addr("10.0.0.2", 1))

	if err := l.allow(addr("10.0.0.3", 1)); !errors.Is(err, udpmw.ErrReject) {
		t.Fatalf("over total: %v", err)
	}

	c.t = c.t.Add(30 * time.Second)
	_ = l.allow(addr("10.0.0.1", 1))
	c.t = c.t.Add(40 * time.Second)

	// 10.0.0.2 expired, 10.0.0.1 was refreshed
	if err := l.allow(addr("10.0.0.3", 1)); err != nil {
		t.Fatal(err)
	}

	if len(l.sessions) != 2 || l.perIP["10.0.0.2"] != 0 {
		t.Fatalf("sessions %d, per ip %v", len(l.sessions), l.perIP)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*SessionLimit{{}, {Max: -1}, {Max: 1, Timeout: -time.Second}} {
		if _, err := m.Middleware(context.Background(), ""); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
