package log

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// Log writes a log line when a connection closes.
type Log struct {
	// Level is debug, info (default), warn or error.
	Level string `cfg:"level"`
}

func (m *Log) Middleware(_ context.Context, name string) (tcpmw.Middleware, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.TrimSpace(m.Level))); err != nil || m.Level == "" {
		level = slog.LevelInfo
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			start := time.Now()
			c := tcpmw.NewCountingConn(conn)

			err := next(c)

			attrs := []any{
				"middleware", name,
				"remote", c.RemoteAddr().String(),
				"local", c.LocalAddr().String(),
				"duration", time.Since(start).String(),
				"bytes_received", c.BytesRead(),
				"bytes_sent", c.BytesWritten(),
			}

			lvl := level
			if err != nil {
				attrs = append(attrs, "err", err.Error())
				lvl = max(lvl, slog.LevelWarn)
			}

			slog.Log(context.Background(), lvl, "tcp connection", attrs...)

			return err
		}
	}, nil
}
