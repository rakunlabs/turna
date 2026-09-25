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
