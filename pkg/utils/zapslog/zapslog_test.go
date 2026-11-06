package zapslog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestZapSlog(t *testing.T) {
	var buf bytes.Buffer
	l := New(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	l.Debug("hidden")
	l.Named("handshake").With(zap.String("a", "1")).Info("hello", zap.Int("n", 2), zap.Error(nil))
	l.Warn("warn", zap.Strings("s", []string{"x"}))

	out := buf.String()
	if strings.Contains(out, "hidden") || !strings.Contains(out, `"a":"1"`) || !strings.Contains(out, `"n":2`) || !strings.Contains(out, `"logger":"handshake"`) || !strings.Contains(out, `"level":"WARN"`) {
		t.Fatal(out)
	}
}
