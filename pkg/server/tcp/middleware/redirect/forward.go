package redirect

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/rakunlabs/turna/pkg/server/proxyproto"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// Forwarder dials an upstream and pipes a client connection to it.
type Forwarder struct {
	Network      string
	DialTimeout  time.Duration
	DisableNagle bool
	// ProxyProtocol version to send to the upstream, 0 disables it.
	ProxyProtocol int
}

// ValidateNetwork checks network and address.
func ValidateNetwork(network, address string) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
		if _, err := net.ResolveTCPAddr(network, address); err != nil {
			return fmt.Errorf("address cannot resolve %s: %w", address, err)
		}
	case "unix", "unixpacket":
		if _, err := net.ResolveUnixAddr(network, address); err != nil {
			return fmt.Errorf("address cannot resolve %s: %w", address, err)
		}
	case "udp", "udp4", "udp6":
		if _, err := net.ResolveUDPAddr(network, address); err != nil {
			return fmt.Errorf("address cannot resolve %s: %w", address, err)
		}
	default:
		return fmt.Errorf("unsupported network %s", network)
	}

	return nil
}

// Dial connects to the upstream address and sends the PROXY header.
func (f *Forwarder) Dial(ctx context.Context, conn net.Conn, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: f.DialTimeout}

	rconn, err := d.DialContext(ctx, f.Network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial to %s: %w", address, err)
	}

	if f.DisableNagle {
		if c := tcpmw.TCPConn(conn); c != nil {
			_ = c.SetNoDelay(true)
		}

		if c, ok := rconn.(*net.TCPConn); ok {
			_ = c.SetNoDelay(true)
		}
	}

	if f.ProxyProtocol > 0 {
		if _, ok := rconn.(*net.TCPConn); ok {
			if err := proxyproto.Write(rconn, f.ProxyProtocol, conn.RemoteAddr(), conn.LocalAddr()); err != nil {
				rconn.Close()

				return nil, fmt.Errorf("failed to write proxy protocol: %w", err)
			}
		}
	}

	return rconn, nil
}

// Forward pipes conn to an already dialed upstream until both sides finish.
func Forward(conn, rconn net.Conn) {
	defer rconn.Close()

	slog.Debug(fmt.Sprintf("connection from %s to %s opened", conn.RemoteAddr(), rconn.RemoteAddr()))

	sent, received := tcpmw.Pipe(conn, rconn)

	slog.Debug(fmt.Sprintf("connection from %s to %s closed", conn.RemoteAddr(), rconn.RemoteAddr()), "sent", sent, "received", received)
}
