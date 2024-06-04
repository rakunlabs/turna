package print

import (
	"bufio"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

type Print struct {
	// Text after to print
	Text string `cfg:"text"`
}

func (m *Print) Middleware() (echo.MiddlewareFunc, error) {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().Method {
			case http.MethodGet:
				return next(c)
			case http.MethodPost:
				body := c.Request().Body
				if body == nil {
					return c.NoContent(http.StatusNoContent)
				}

				if _, err := bufio.NewReader(body).WriteTo(os.Stderr); err != nil {
					return err
				}

				if m.Text != "" {
					if _, err := os.Stderr.Write([]byte(m.Text)); err != nil {
						return err
					}
				}

				os.Stderr.WriteString("\n")

				return c.NoContent(http.StatusNoContent)
			default:
				return c.String(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
			}
		}
	}, nil
}
