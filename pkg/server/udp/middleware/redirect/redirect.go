package redirect

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Redirect proxies a single request/response datagram to an upstream UDP
// server. For each incoming datagram it dials the target, forwards the
// payload, waits for one reply within Timeout and writes it back to the peer.
//
// This fits single request/response protocols such as DNS. Protocols that
// answer with multiple datagrams per request are not supported.
type Redirect struct {
	Address string `cfg:"address"`
	Network string `cfg:"network"`

	// Timeout for dialing the upstream and waiting for the reply.
	Timeout time.Duration `cfg:"timeout"`
	// Buffer size for the upstream reply, default is 65535.
	Buffer int `cfg:"buffer"`
}

func (m *Redirect) Middleware(ctx context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	network := m.Network
	if network == "" {
		network = "udp"
	}

	if !strings.HasPrefix(network, "udp") {
		return nil, fmt.Errorf("unsupported network %s, only udp is supported", network)
	}

	if _, err := net.ResolveUDPAddr(network, m.Address); err != nil {
		return nil, fmt.Errorf("address cannot resolve %s: %w", m.Address, err)
	}

	buffer := m.Buffer
	if buffer <= 0 {
		buffer = 65535
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return func(conn net.PacketConn, addr net.Addr, data []byte) error {
		dialCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		d := net.Dialer{}
		rconn, err := d.DialContext(dialCtx, network, m.Address)
		if err != nil {
			return fmt.Errorf("failed to dial to %s: %w", m.Address, err)
		}
		defer rconn.Close()

		_ = rconn.SetDeadline(time.Now().Add(timeout))

		if _, err := rconn.Write(data); err != nil {
			return fmt.Errorf("failed to write to %s: %w", m.Address, err)
		}

		rbuf := make([]byte, buffer)
		n, err := rconn.Read(rbuf)
		if err != nil {
			return fmt.Errorf("failed to read from %s: %w", m.Address, err)
		}

		if _, err := conn.WriteTo(rbuf[:n], addr); err != nil {
			return fmt.Errorf("failed to write response to %s: %w", addr, err)
		}

		return nil
	}, nil
}
