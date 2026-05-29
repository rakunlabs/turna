package gzip

import (
	"net/http"

	"github.com/rakunlabs/ada/middleware/encoding"
)

type Gzip struct {
}

func (m *Gzip) Middleware() func(http.Handler) http.Handler {
	return encoding.Middleware(encoding.WithConfig(encoding.Config{
		Encoding: []string{"gzip"},
	}))
}
