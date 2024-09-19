package log

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type Log struct {
	Level   string `cfg:"level"`
	Message string `cfg:"message"`
	Headers bool   `cfg:"headers"`
}

func (m *Log) LevelCheck() error {
	if m.Level == "" {
		m.Level = "info"
	}

	m.Level = strings.ToLower(m.Level)

	for _, v := range []string{"debug", "info", "warn", "error"} {
		if m.Level == v {
			return nil
		}
	}

	return fmt.Errorf("invalid log level: %s", m.Level)
}

func (m *Log) Middleware() (func(http.Handler) http.Handler, error) {
	if err := m.LevelCheck(); err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			args := []interface{}{
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", r.Header.Get("X-Request-Id"),
			}

			// get headers
			var headers map[string][]string
			if m.Headers {
				headers = make(map[string][]string, len(r.Header))
				for k, v := range r.Header {
					headers[k] = v
				}

				args = append(args, "headers", headers)
			}

			switch m.Level {
			case "debug":
				slog.Debug(m.Message, args...)
			case "info":
				slog.Info(m.Message, args...)
			case "warn":
				slog.Warn(m.Message, args...)
			case "error":
				slog.Error(m.Message, args...)
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
