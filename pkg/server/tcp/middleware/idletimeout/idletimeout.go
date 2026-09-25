package idletimeout

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// IdleTimeout closes connections without traffic for Timeout. MaxDuration
// limits the whole connection lifetime.
type IdleTimeout struct {
	Timeout     time.Duration `cfg:"timeout"`
	MaxDuration time.Duration `cfg:"max_duration"`
}

func (m *IdleTimeout) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if m.Timeout <= 0 && m.MaxDuration <= 0 {
		return nil, errors.New("timeout or max_duration is required")
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			c := &Conn{Conn: conn, timeout: m.Timeout}
			if m.MaxDuration > 0 {
				c.deadline = time.Now().Add(m.MaxDuration)
			}

			c.extend()

			return next(c)
		}
	}, nil
}

// Conn extends its deadline on every read and write. Both directions share
// the deadline so a connection stays open while either side sends data.
type Conn struct {
	net.Conn

	timeout  time.Duration
	deadline time.Time
}

func (c *Conn) extend() {
	var d time.Time
	if c.timeout > 0 {
		d = time.Now().Add(c.timeout)
	}

	if !c.deadline.IsZero() && (d.IsZero() || c.deadline.Before(d)) {
		d = c.deadline
	}

	_ = c.Conn.SetDeadline(d)
}

func (c *Conn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.extend()
	}

	return n, err
}

func (c *Conn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.extend()
	}

	return n, err
}

func (c *Conn) NetConn() net.Conn { return c.Conn }
