// Package tcpmw defines the TCP middleware chain types and connection helpers
// shared by the TCP server and its middlewares.
package tcpmw

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync/atomic"
)

// ErrReject marks an expected rejection (rate limit, deny list); the server
// logs it at debug level only.
var ErrReject = errors.New("connection rejected")

// Handler handles an accepted connection. The server closes the connection
// after the chain returns.
type Handler = func(conn net.Conn) error

// Middleware wraps the next handler. Filters call next; terminal middlewares
// such as redirect or socks5 consume the connection and ignore next.
type Middleware = func(next Handler) Handler

// Filter turns a check into a middleware that calls next when check passes.
func Filter(check func(conn net.Conn) error) Middleware {
	return func(next Handler) Handler {
		return func(conn net.Conn) error {
			if err := check(conn); err != nil {
				return err
			}

			return next(conn)
		}
	}
}

// Chain builds the handler of middlewares, the first middleware runs first.
func Chain(final Handler, middlewares ...Middleware) Handler {
	h := final
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}

// Unwrapper is implemented by connection wrappers, *tls.Conn included.
type Unwrapper interface {
	NetConn() net.Conn
}

// TCPConn returns the underlying *net.TCPConn of conn, or nil.
func TCPConn(conn net.Conn) *net.TCPConn {
	for conn != nil {
		if c, ok := conn.(*net.TCPConn); ok {
			return c
		}

		u, ok := conn.(Unwrapper)
		if !ok {
			return nil
		}

		conn = u.NetConn()
	}

	return nil
}

// CloseWrite half-closes conn when supported, otherwise it closes conn.
// Wrappers are unwrapped until a connection with CloseWrite is found, so a
// *tls.Conn sends close_notify instead of half-closing the raw socket.
func CloseWrite(conn net.Conn) error {
	for c := conn; c != nil; {
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			return cw.CloseWrite()
		}

		u, ok := c.(Unwrapper)
		if !ok {
			break
		}

		c = u.NetConn()
	}

	return conn.Close()
}

// Conn wraps a net.Conn and can replace its reader and remote address.
type Conn struct {
	net.Conn

	Reader     io.Reader
	Remote     net.Addr
	LocalAddrV net.Addr
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.Reader != nil {
		return c.Reader.Read(b)
	}

	return c.Conn.Read(b)
}

func (c *Conn) RemoteAddr() net.Addr {
	if c.Remote != nil {
		return c.Remote
	}

	return c.Conn.RemoteAddr()
}

func (c *Conn) LocalAddr() net.Addr {
	if c.LocalAddrV != nil {
		return c.LocalAddrV
	}

	return c.Conn.LocalAddr()
}

func (c *Conn) NetConn() net.Conn {
	return c.Conn
}

// BufferedConn returns a connection reading through a bufio.Reader so that
// data can be peeked without being consumed.
func BufferedConn(conn net.Conn, size int) (*Conn, *bufio.Reader) {
	if c, ok := conn.(*Conn); ok {
		if br, ok := c.Reader.(*bufio.Reader); ok {
			return c, br
		}
	}

	br := bufio.NewReaderSize(conn, size)

	return &Conn{Conn: conn, Reader: br}, br
}

// Pipe copies data in both directions and returns the number of bytes sent to
// and received from the upstream.
//
// When a side reaches EOF the other side is half-closed so request/response
// protocols can finish. When a copy fails, both connections are closed to
// stop the other direction.
func Pipe(client, upstream net.Conn) (sent, received int64) {
	type result struct {
		n   int64
		err error
	}

	done := make(chan result, 1)

	go func() {
		n, err := io.Copy(upstream, client)
		if err == nil {
			_ = CloseWrite(upstream)
		} else {
			_ = upstream.Close()
			_ = client.Close()
		}

		done <- result{n, err}
	}()

	received, err := io.Copy(client, upstream)
	if err == nil {
		_ = CloseWrite(client)
	} else {
		_ = upstream.Close()
		_ = client.Close()
	}

	r := <-done

	return r.n, received
}

// CountingConn counts the bytes read from and written to a connection.
type CountingConn struct {
	net.Conn

	read    atomic.Int64
	written atomic.Int64
}

func NewCountingConn(conn net.Conn) *CountingConn {
	return &CountingConn{Conn: conn}
}

func (c *CountingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Add(int64(n))

	return n, err
}

func (c *CountingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.written.Add(int64(n))

	return n, err
}

func (c *CountingConn) NetConn() net.Conn { return c.Conn }

// BytesRead returns the bytes received from the peer.
func (c *CountingConn) BytesRead() int64 { return c.read.Load() }

// BytesWritten returns the bytes sent to the peer.
func (c *CountingConn) BytesWritten() int64 { return c.written.Load() }
