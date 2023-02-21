package middlewares

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

type Hello struct {
	// Message to return, default is "OK"
	Message string `cfg:"message"`
	// Status code to return, default is 200
	StatusCode int               `cfg:"status_code"`
	Headers    map[string]string `cfg:"headers"`

	// Type of return (json, json-pretty, html, string), default is string
	Type string `cfg:"type"`
}

func (h *Hello) Middleware() echo.MiddlewareFunc {
	if h.StatusCode == 0 {
		h.StatusCode = http.StatusOK
	}

	if h.Message == "" {
		h.Message = "OK"
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for k, v := range h.Headers {
				c.Response().Header().Set(k, v)
			}

			switch strings.ToLower(h.Type) {
			case "json-pretty":
				return c.JSONPretty(h.StatusCode, json.RawMessage(h.Message), "  ")
			case "json":
				return c.JSON(h.StatusCode, json.RawMessage(h.Message))
			case "html":
				return c.HTML(h.StatusCode, h.Message)
			default:
				return c.String(h.StatusCode, h.Message)
			}
		}
	}
}
