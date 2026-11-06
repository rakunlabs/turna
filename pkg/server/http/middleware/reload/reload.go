// Package reload reloads the configuration of Turna on request, like
// SIGHUP. Protect the route with auth, ip_filter or similar middlewares.
package reload

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/reloader"
)

type Reload struct {
	// Methods allowed to trigger the reload, default is POST.
	Methods []string `cfg:"methods"`
	// Timeout of the reload, default 1m.
	Timeout time.Duration `cfg:"timeout"`
}

type response struct {
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

func (m *Reload) Middleware() (func(http.Handler) http.Handler, error) {
	methods := make([]string, 0, len(m.Methods))
	for _, method := range m.Methods {
		if method = strings.ToUpper(strings.TrimSpace(method)); method != "" {
			methods = append(methods, method)
		}
	}

	if len(methods) == 0 {
		methods = []string{http.MethodPost}
	}

	if m.Timeout < 0 {
		return nil, errors.New("reload timeout must not be negative")
	}

	timeout := m.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}

	allow := strings.Join(methods, ", ")

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(methods, r.Method) {
				w.Header().Set("Allow", allow)
				httputil.HandleError(w, httputil.NewError("method not allowed", nil, http.StatusMethodNotAllowed))

				return
			}

			// the reload must finish even when the client goes away
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
			defer cancel()

			if err := reloader.Reload(ctx, "http"); err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, reloader.ErrNotAvailable) {
					status = http.StatusServiceUnavailable
				}

				slog.Warn("reload request failed", "remote", r.RemoteAddr, "err", err.Error())
				_ = httputil.JSON(w, status, response{Message: "reload failed", Error: err.Error()})

				return
			}

			_ = httputil.JSON(w, http.StatusOK, response{Message: "configuration reloaded"})
		})
	}, nil
}
