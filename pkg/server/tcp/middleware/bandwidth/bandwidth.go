package bandwidth

import (
	"context"
	"errors"
	"net"

	"golang.org/x/time/rate"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// BandwidthLimit limits the transfer rate in bytes per second of each
// connection. Upload is data from the client, download is data to it.
type BandwidthLimit struct {
	Upload   int `cfg:"upload"`
	Download int `cfg:"download"`
	// Burst in bytes, default is the rate (1 second).
	Burst int `cfg:"burst"`
}

func (m *BandwidthLimit) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if m.Upload < 0 || m.Download < 0 || m.Burst < 0 {
		return nil, errors.New("upload, download and burst must not be negative")
	}

	if m.Upload == 0 && m.Download == 0 {
		return nil, errors.New("upload or download is required")
	}

	newLimiter := func(r int) *rate.Limiter {
		if r == 0 {
			return nil
		}

		burst := m.Burst
		if burst == 0 {
			burst = r
		}

		return rate.NewLimiter(rate.Limit(r), burst)
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			return next(&Conn{
				Conn:  conn,
				read:  newLimiter(m.Upload),
				write: newLimiter(m.Download),
			})
		}
	}, nil
}

type Conn struct {
	net.Conn

	read  *rate.Limiter
	write *rate.Limiter
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.read != nil && len(b) > c.read.Burst() {
		b = b[:c.read.Burst()]
	}

	n, err := c.Conn.Read(b)
	if n > 0 && c.read != nil {
		if werr := c.read.WaitN(context.Background(), n); werr != nil && err == nil {
			err = werr
		}
	}

	return n, err
}

func (c *Conn) Write(b []byte) (int, error) {
	if c.write == nil {
		return c.Conn.Write(b)
	}

	written := 0

	for len(b) > 0 {
		chunk := min(len(b), c.write.Burst())

		if err := c.write.WaitN(context.Background(), chunk); err != nil {
			return written, err
		}

		n, err := c.Conn.Write(b[:chunk])
		written += n

		if err != nil {
			return written, err
		}

		b = b[chunk:]
	}

	return written, nil
}

func (c *Conn) NetConn() net.Conn { return c.Conn }
