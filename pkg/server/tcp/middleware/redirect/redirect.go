package redirect

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type Redirect struct {
	Address string `cfg:"address"`
	Network string `cfg:"network"`

	DisableNagle bool `cfg:"disable_nagle"`
	// Buffer is deprecated and ignored; data is copied with the kernel
	// splice/sendfile fast path when available.
	Buffer int `cfg:"buffer"`

	DialTimeout time.Duration `cfg:"dial_timeout"`

	// ProxyProtocol sends a PROXY protocol header to the upstream.
	ProxyProtocol bool `cfg:"proxy_protocol"`
	// ProxyProtocolVersion is 1 (text) or 2 (binary), default is 1.
	ProxyProtocolVersion int `cfg:"proxy_protocol_version"`
}

// Forwarder returns the forwarder of the configuration.
func (m *Redirect) Forwarder() (*Forwarder, error) {
	network := m.Network
	if network == "" {
		network = "tcp"
	}

	f := &Forwarder{
		Network:      network,
		DialTimeout:  m.DialTimeout,
		DisableNagle: m.DisableNagle,
	}

	if m.ProxyProtocol {
		switch m.ProxyProtocolVersion {
		case 0, 1:
			f.ProxyProtocol = 1
		case 2:
			f.ProxyProtocol = 2
		default:
			return nil, fmt.Errorf("unsupported proxy_protocol_version %d", m.ProxyProtocolVersion)
		}
	}

	return f, nil
}

func (m *Redirect) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	f, err := m.Forwarder()
	if err != nil {
		return nil, err
	}

	if err := ValidateNetwork(f.Network, m.Address); err != nil {
		return nil, err
	}

	return func(_ tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			rconn, err := f.Dial(ctx, conn, m.Address)
			if err != nil {
				return err
			}

			Forward(conn, rconn)

			return nil
		}
	}, nil
}
