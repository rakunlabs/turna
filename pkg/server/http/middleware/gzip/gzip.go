package gzip

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type Gzip struct {
	Level int `cfg:"level"`
}

func (m *Gzip) Middleware() func(http.Handler) http.Handler {
	level := m.Level
	if level == 0 {
		level = 5
	}

	return middleware.Compress(level)
}
