package log

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
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

func (m *Log) Middleware() ([]echo.MiddlewareFunc, error) {
	if err := m.LevelCheck(); err != nil {
		return nil, err
	}

	return []echo.MiddlewareFunc{func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			args := []interface{}{
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"request_id", c.Request().Header.Get("X-Request-Id"),
			}

			// get headers
			var headers map[string][]string
			if m.Headers {
				headers = make(map[string][]string, len(c.Request().Header))
				for k, v := range c.Request().Header {
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

			return next(c)
		}
	}}, nil
}
