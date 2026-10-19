package gzip

import (
	"log/slog"
	"net/http"

	"github.com/rakunlabs/ada/middleware/encoding"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// Gzip is deprecated, use compress.
type Gzip struct {
}

func (m *Gzip) Middleware() func(http.Handler) http.Handler {
	slog.Warn("gzip middleware is deprecated, use compress instead")

	encodingMiddleware := encoding.Middleware(encoding.WithConfig(encoding.Config{
		Encoding: []string{"gzip"},
	}))

	return func(next http.Handler) http.Handler {
		encoded := encodingMiddleware(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket upgrades need the raw ResponseWriter so the proxy can
			// hijack the connection; compressing would break the upgrade.
			if httputil.IsWebSocket(r) {
				next.ServeHTTP(w, r)

				return
			}

			encoded.ServeHTTP(w, r)
		})
	}
}
