// Package udpmw holds types shared by the UDP server and its middlewares.
package udpmw

import (
	"errors"
	"net"
)

// ErrReject marks an expected drop (rate limit, deny list); the server logs
// it at debug level only.
var ErrReject = errors.New("packet rejected")

// Handler processes a single datagram. Returning an error stops the chain.
type Handler = func(conn net.PacketConn, addr net.Addr, data []byte) error

// Conn is the socket handed to the handlers of one datagram. A handler can
// replace the underlying socket for the handlers after it, e.g. to limit
// replies; middlewares keeping the socket for later replies keep the Conn.
type Conn struct {
	net.PacketConn
}

// Wrap replaces the socket of conn with wrap(socket) for the handlers after
// the caller. It reports false when conn is not a *Conn.
func Wrap(conn net.PacketConn, wrap func(net.PacketConn) net.PacketConn) bool {
	c, ok := conn.(*Conn)
	if !ok {
		return false
	}

	c.PacketConn = wrap(c.PacketConn)

	return true
}
