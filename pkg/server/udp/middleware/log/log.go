package log

import (
	"context"
	"log/slog"
	"net"
	"strings"
)

// Log writes a log line for every datagram. Place it first in the chain.
type Log struct {
	// Level is debug (default), info, warn or error.
	Level string `cfg:"level"`
}

func (m *Log) Middleware(_ context.Context, name string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	level := slog.LevelDebug
	if m.Level != "" {
		if err := level.UnmarshalText([]byte(strings.TrimSpace(m.Level))); err != nil {
			return nil, err
		}
	}

	return func(conn net.PacketConn, addr net.Addr, data []byte) error {
		slog.Log(context.Background(), level, "udp packet",
			"middleware", name,
			"remote", addr.String(),
			"local", conn.LocalAddr().String(),
			"size", len(data),
		)

		return nil
	}, nil
}
