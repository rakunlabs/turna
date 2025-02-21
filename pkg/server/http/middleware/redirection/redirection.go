package redirection

import (
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

type Redirection struct {
	URL       string `cfg:"url"`
	Permanent bool   `cfg:"permanent"`
}

func (m *Redirection) Middleware() func(http.Handler) http.Handler {
	statusCode := http.StatusTemporaryRedirect
	if m.URL == "" {
		m.URL = "/"
	}
	if m.Permanent {
		statusCode = http.StatusPermanentRedirect
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httputil.Redirect(w, statusCode, m.URL)
		})
	}
}
