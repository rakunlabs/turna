package middlewares

import (
	"bufio"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

type Print struct {
	// To for stdout or stderr, default stdout
	To string `cfg:"to"`

	// Text after to print
	Text string `cfg:"text"`
}

func (m *Print) Middleware() (echo.MiddlewareFunc, error) {
	to := os.Stdout
	if strings.EqualFold(m.To, "stderr") {
		to = os.Stderr
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().Method {
			case http.MethodGet:
				return next(c)
			case http.MethodPost:
				if _, err := bufio.NewReader(c.Request().Body).WriteTo(to); err != nil {
					return err
				}

				if m.Text != "" {
					if _, err := to.Write([]byte(m.Text)); err != nil {
						return err
					}
				}

				return c.NoContent(http.StatusNoContent)
			default:
				return c.String(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
			}
		}
	}, nil
}
