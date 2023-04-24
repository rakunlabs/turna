package middlewares

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
			// get headers
			var headers map[string][]string
			if m.Headers {
				headers = make(map[string][]string, len(c.Request().Header))
				for k, v := range c.Request().Header {
					headers[k] = v
				}
			}

			logger := log.Ctx(c.Request().Context())
			var logEvent *zerolog.Event
			switch m.Level {
			case "debug":
				logEvent = logger.Debug()
			case "info":
				logEvent = logger.Info()
			case "warn":
				logEvent = logger.Warn()
			case "error":
				logEvent = logger.Error()
			}

			event := logEvent.Str("method", c.Request().Method).Str("path", c.Request().URL.Path)
			if m.Headers {
				event = event.Interface("headers", headers)
			}

			event.Msg(m.Message)

			return next(c)
		}
	}}, nil
}
