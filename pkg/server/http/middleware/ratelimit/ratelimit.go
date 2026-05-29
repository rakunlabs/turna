package ratelimit

import (
	"net/http"
	"strings"
	"time"

	adaratelimit "github.com/rakunlabs/ada/middleware/ratelimit"
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
		handler = adaratelimit.LimitByIP(m.Requests, m.Duration)
	case "realip":
		handler = adaratelimit.LimitByRealIP(m.Requests, m.Duration)
	default: // all
		handler = adaratelimit.LimitAll(m.Requests, m.Duration)
	}

	return handler
}
