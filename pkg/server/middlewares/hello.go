package middlewares

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"github.com/rytsh/liz/fstore"
	"github.com/rytsh/liz/templatex"
	"github.com/rytsh/liz/templatex/store"
	"github.com/worldline-go/logz"
)

type Hello struct {
	// Message to return, default is "OK"
	Message string `cfg:"message"`
	// Status code to return, default is 200
	StatusCode int               `cfg:"status_code"`
	Headers    map[string]string `cfg:"headers"`

	// Type of return (json, json-pretty, html, string), default is string
	Type string `cfg:"type"`

	// Template to render go template
	Template bool `cfg:"template"`
	// Trust is allow to use powerfull functions
	Trust bool `cfg:"trust"`
	// WorkDir is the directory for some functions
	WorkDir string `cfg:"work_dir"`
	// Delims is the delimiters for the template
	Delims []string `cfg:"delims"`
}

func (h *Hello) Middleware() ([]echo.MiddlewareFunc, error) {
	if h.Delims != nil {
		if len(h.Delims) != 2 {
			return nil, fmt.Errorf("delims must be a pair of strings")
		}
	}

	if h.StatusCode == 0 {
		h.StatusCode = http.StatusOK
	}

	if h.Message == "" {
		h.Message = "OK"
	}

	tpl := templatex.New(store.WithAddFuncsTpl(
		fstore.FuncMapTpl(
			fstore.WithLog(logz.AdapterKV{Log: log.Logger}),
			fstore.WithTrust(h.Trust),
			fstore.WithWorkDir(h.WorkDir),
		),
	))

	if h.Delims != nil {
		tpl.SetDelims(h.Delims[0], h.Delims[1])
	}

	return []echo.MiddlewareFunc{func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			for k, v := range h.Headers {
				c.Response().Header().Set(k, v)
			}

			message := h.Message

			if h.Template {
				// max body size is 4MB
				body, error := io.ReadAll(io.LimitReader(c.Request().Body, 1<<22))
				if error != nil {
					return error
				}

				c.Request().Body.Close()

				// get all data for the template
				data := map[string]interface{}{
					"body":         body,
					"method":       c.Request().Method,
					"headers":      c.Request().Header,
					"query_params": c.QueryParams(),
					"cookies":      c.Cookies(),
					"path":         c.Request().URL.Path,
				}

				v, err := tpl.ExecuteBuffer(
					templatex.WithContent(message),
					templatex.WithData(data),
				)

				if err != nil {
					return err
				}

				message = string(v)
			}

			switch strings.ToLower(h.Type) {
			case "json-pretty":
				return c.JSONPretty(h.StatusCode, json.RawMessage(message), "  ")
			case "json":
				return c.JSON(h.StatusCode, json.RawMessage(message))
			case "html":
				return c.HTML(h.StatusCode, message)
			default:
				return c.String(h.StatusCode, message)
			}
		}
	}}, nil
}
