// Package zapslog forwards zap log entries to a slog logger, for libraries
// such as certmagic that only accept a *zap.Logger.
package zapslog

import (
	"context"
	"log/slog"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a zap logger writing to l, slog.Default when l is nil.
func New(l *slog.Logger) *zap.Logger {
	if l == nil {
		l = slog.Default()
	}

	return zap.New(&core{l: l})
}

type core struct {
	l     *slog.Logger
	attrs []any
}

func level(l zapcore.Level) slog.Level {
	switch {
	case l <= zapcore.DebugLevel:
		return slog.LevelDebug
	case l == zapcore.InfoLevel:
		return slog.LevelInfo
	case l == zapcore.WarnLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

func (c *core) Enabled(l zapcore.Level) bool {
	return c.l.Enabled(context.Background(), level(l))
}

func (c *core) With(fields []zapcore.Field) zapcore.Core {
	return &core{l: c.l, attrs: append(append([]any{}, c.attrs...), toAttrs(fields)...)}
}

func (c *core) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}

	return ce
}

func (c *core) Write(e zapcore.Entry, fields []zapcore.Field) error {
	attrs := append(append([]any{}, c.attrs...), toAttrs(fields)...)
	if e.LoggerName != "" {
		attrs = append(attrs, "logger", e.LoggerName)
	}

	c.l.Log(context.Background(), level(e.Level), e.Message, attrs...)

	return nil
}

func (c *core) Sync() error { return nil }

func toAttrs(fields []zapcore.Field) []any {
	if len(fields) == 0 {
		return nil
	}

	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}

	attrs := make([]any, 0, 2*len(enc.Fields))
	for k, v := range enc.Fields {
		attrs = append(attrs, k, v)
	}

	return attrs
}
