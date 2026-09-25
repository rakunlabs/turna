package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"strings"
)

// Mirror sends a copy of every datagram to shadow servers. Replies of the
// shadow servers are ignored and the chain continues.
type Mirror struct {
	Servers []Server `cfg:"servers"`
	Network string   `cfg:"network"`
}

type Server struct {
	Address string `cfg:"address"`
	// Percent of datagrams to mirror, default is 100.
	Percent float64 `cfg:"percent"`
}

type target struct {
	addr    *net.UDPAddr
	percent float64
}

func (m *Mirror) Middleware(ctx context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	if len(m.Servers) == 0 {
		return nil, errors.New("servers are required")
	}

	network := m.Network
	if network == "" {
		network = "udp"
	}

	if !strings.HasPrefix(network, "udp") {
		return nil, fmt.Errorf("unsupported network %s, only udp is supported", network)
	}

	targets := make([]target, 0, len(m.Servers))

	for _, s := range m.Servers {
		addr, err := net.ResolveUDPAddr(network, s.Address)
		if err != nil {
			return nil, fmt.Errorf("address cannot resolve %s: %w", s.Address, err)
		}

		percent := s.Percent
		if percent <= 0 || percent > 100 {
			percent = 100
		}

		targets = append(targets, target{addr: addr, percent: percent})
	}

	// one unconnected socket sends all mirror traffic, replies are drained
	out, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, fmt.Errorf("mirror socket: %w", err)
	}

	go func() {
		<-ctx.Done()
		out.Close()
	}()

	go func() {
		buf := make([]byte, 65535)
		for {
			if _, _, err := out.ReadFrom(buf); errors.Is(err, net.ErrClosed) {
				return
			}
		}
	}()

	return func(_ net.PacketConn, _ net.Addr, data []byte) error {
		for _, t := range targets {
			if t.percent < 100 && rand.Float64()*100 >= t.percent { //nolint:gosec // sampling
				continue
			}

			if _, err := out.WriteToUDP(data, t.addr); err != nil && !errors.Is(err, net.ErrClosed) {
				slog.Debug("udp mirror write failed", "target", t.addr.String(), "err", err.Error())
			}
		}

		return nil
	}, nil
}
