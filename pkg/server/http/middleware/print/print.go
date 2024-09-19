package print

import (
	"bufio"
	"net/http"
	"os"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Print struct {
	// Text after to print
	Text string `cfg:"text"`
}

func (m *Print) Middleware() (func(http.Handler) http.Handler, error) {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				next.ServeHTTP(w, r)
				return
			case http.MethodPost:
				body := r.Body
				if body == nil {
					httputil.NoContent(w, http.StatusNoContent)
					return
				}

				if _, err := bufio.NewReader(body).WriteTo(os.Stderr); err != nil {
					httputil.HandleError(w, httputil.NewError("failed to write to stderr", err, http.StatusInternalServerError))
					return
				}

				if m.Text != "" {
					if _, err := os.Stderr.Write([]byte(m.Text)); err != nil {
						httputil.HandleError(w, httputil.NewError("failed to write to stderr", err, http.StatusInternalServerError))
						return
					}
				}

				os.Stderr.WriteString("\n")

				httputil.NoContent(w, http.StatusNoContent)
				return
			default:
				httputil.HandleError(w, httputil.NewError("method not allowed", nil, http.StatusMethodNotAllowed))
				return
			}
		})
	}, nil
}
