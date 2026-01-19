package ratelimit

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

type RateLimit struct {
	// LimitType can be All, IP or RealIP; default is All
	LimitType string        `cfg:"limit_type"`
	Requests  int           `cfg:"requests" default:"100"`
	Duration  time.Duration `cfg:"duration" default:"1m"`
}

func (m *RateLimit) Middleware() func(http.Handler) http.Handler {
	var handler func(http.Handler) http.Handler

	switch strings.ToLower(strings.TrimSpace(m.LimitType)) {
	case "ip":
		handler = httprate.LimitByIP(m.Requests, m.Duration)
	case "realip":
		handler = httprate.LimitByRealIP(m.Requests, m.Duration)
	default: // all
		handler = httprate.LimitAll(m.Requests, m.Duration)
	}

	return handler
}
