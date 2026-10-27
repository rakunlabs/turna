// Package mirror copies the client side of TCP connections to shadow
// servers. Their replies are discarded and they never slow the real
// connection down.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type Mirror struct {
	Servers []Server `cfg:"servers"`
	// DialTimeout of the shadow servers, default 2s.
	DialTimeout time.Duration `cfg:"dial_timeout"`
	// Buffer is the data queued per shadow connection, default 1MB. When the
	// shadow is slower, its copy stops.
	Buffer int `cfg:"buffer"`
}

type Server struct {
	Address string `cfg:"address"`
	// Percent of connections to mirror, default is 100.
	Percent float64 `cfg:"percent"`
}

func (m *Mirror) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	if len(m.Servers) == 0 {
		return nil, errors.New("servers are required")
	}

	for i, s := range m.Servers {
		if err := redirect.ValidateNetwork("tcp", s.Address); err != nil {
			return nil, err
		}

		if s.Percent < 0 || s.Percent > 100 {
			return nil, fmt.Errorf("server %d percent must be between 0 and 100", i)
		}

		if s.Percent == 0 {
			m.Servers[i].Percent = 100
		}
	}

	if m.DialTimeout < 0 || m.Buffer < 0 {
		return nil, errors.New("dial_timeout and buffer must not be negative")
	}

	dialTimeout := m.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 2 * time.Second
	}

	buffer := m.Buffer
	if buffer == 0 {
		buffer = 1 << 20
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			var shadows []*shadow

			for _, s := range m.Servers {
				if s.Percent < 100 && rand.Float64()*100 >= s.Percent { //nolint:gosec // sampling
					continue
				}

				shadows = append(shadows, newShadow(ctx, s.Address, dialTimeout, buffer))
			}

			if len(shadows) == 0 {
				return next(conn)
			}

			defer func() {
				for _, s := range shadows {
					s.finish()
				}
			}()

			return next(&mirrorConn{Conn: conn, shadows: shadows})
		}
	}, nil
}

// mirrorConn copies everything read from the client to the shadows.
type mirrorConn struct {
	net.Conn

	shadows []*shadow
}

func (c *mirrorConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		for _, s := range c.shadows {
			s.send(b[:n])
		}
	}

	return n, err
}

// WriteTo hides the io.WriterTo of the connection so io.Copy reads through
// Read and the data is mirrored.
func (c *mirrorConn) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, struct{ io.Reader }{c})
}

func (c *mirrorConn) NetConn() net.Conn {
	return c.Conn
}

// shadow dials and writes to one shadow server in the background.
type shadow struct {
	mu      sync.Mutex
	queue   [][]byte
	queued  int
	limit   int
	dropped bool
	done    bool
	wake    chan struct{}
}

func newShadow(ctx context.Context, address string, dialTimeout time.Duration, limit int) *shadow {
	s := &shadow{limit: limit, wake: make(chan struct{}, 1)}

	go s.run(ctx, address, dialTimeout)

	return s
}

func (s *shadow) send(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dropped || s.done {
		return
	}

	if s.queued+len(b) > s.limit {
		// the shadow cannot keep up, a partial stream is useless
		s.dropped = true
		s.queue = nil
		s.queued = 0
		s.signal()

		return
	}

	s.queue = append(s.queue, append([]byte(nil), b...))
	s.queued += len(b)
	s.signal()
}

func (s *shadow) finish() {
	s.mu.Lock()
	s.done = true
	s.signal()
	s.mu.Unlock()
}

func (s *shadow) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *shadow) take() (data [][]byte, stop, closeWrite bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, s.queue, s.queued = s.queue, nil, 0

	return data, s.dropped, s.done
}

func (s *shadow) run(ctx context.Context, address string, dialTimeout time.Duration) {
	d := net.Dialer{Timeout: dialTimeout}

	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		slog.Debug("tcp mirror dial failed", "target", address, "err", err.Error())
		s.mu.Lock()
		s.dropped = true
		s.queue = nil
		s.mu.Unlock()

		return
	}
	defer conn.Close()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(io.Discard, conn)
	}()

	for {
		data, stop, closeWrite := s.take()

		for _, b := range data {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := conn.Write(b); err != nil {
				slog.Debug("tcp mirror write failed", "target", address, "err", err.Error())

				return
			}
		}

		if stop {
			return
		}

		// send adds nothing after finish, so the queue is drained
		if closeWrite {
			_ = tcpmw.CloseWrite(conn)

			// unread replies would make Close send a reset
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			<-drained

			return
		}

		select {
		case <-s.wake:
		case <-ctx.Done():
			return
		}
	}
}
