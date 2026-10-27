package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/requestkey"
)

type RateLimit struct {
	// LimitType can be All, IP or RealIP; default is All. Ignored when Key is set.
	LimitType string        `cfg:"limit_type"`
	Requests  int           `cfg:"requests" default:"100"`
	Duration  time.Duration `cfg:"duration" default:"1m"`

	// Key selects what requests are counted by, parts are combined:
	//   ip, realip, path, host, method, header:<name>, query:<name>, cookie:<name>
	// A part missing in the request falls back to the client IP.
	Key []string `cfg:"key"`
	// TrustedProxies are CIDRs whose forwarding headers are used for the ip key.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// Headers adds X-RateLimit-Limit, X-RateLimit-Remaining and X-RateLimit-Reset to responses.
	Headers bool `cfg:"headers"`
	// Redis shares the counters between instances; default is in memory.
	Redis *Redis `cfg:"redis"`
	// FailClosed rejects requests with 503 when Redis is unavailable,
	// default lets them through.
	FailClosed bool `cfg:"fail_closed"`
}

func (m *RateLimit) keyFunc() (func(r *http.Request) string, error) {
	keys := m.Key
	if len(keys) == 0 {
		switch strings.ToLower(strings.TrimSpace(m.LimitType)) {
		case "ip":
			keys = []string{"ip"}
		case "realip":
			keys = []string{"realip"}
		case "", "all":
		default:
			return nil, fmt.Errorf("rate_limit limit_type %q is unknown, expected all, ip or realip", m.LimitType)
		}
	}

	return requestkey.New("rate_limit", keys, m.TrustedProxies)
}

func (m *RateLimit) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if m.Requests <= 0 {
		return nil, errors.New("rate_limit requests must be greater than 0")
	}

	if m.Duration <= 0 {
		return nil, errors.New("rate_limit duration must be greater than 0")
	}

	keyFunc, err := m.keyFunc()
	if err != nil {
		return nil, err
	}

	var s store = newMemoryStore(m.Requests, m.Duration)
	if m.Redis != nil {
		if s, err = newRedisStore(ctx, m.Redis, m.Requests, m.Duration); err != nil {
			return nil, err
		}
	}

	return m.handler(s, keyFunc), nil
}

func (m *RateLimit) handler(s store, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	limit := strconv.Itoa(m.Requests)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := s.allow(r.Context(), keyFunc(r))
			if err != nil {
				if r.Context().Err() != nil {
					return
				}

				slog.Error("rate_limit store failed", "error", err)

				if m.FailClosed {
					httputil.HandleError(w, httputil.NewError("rate limiter unavailable", nil, http.StatusServiceUnavailable))

					return
				}

				next.ServeHTTP(w, r)

				return
			}

			reset := formatSeconds(res.reset)

			if m.Headers {
				h := w.Header()
				h.Set("X-RateLimit-Limit", limit)
				h.Set("X-RateLimit-Remaining", strconv.Itoa(res.remaining))
				h.Set("X-RateLimit-Reset", reset)
			}

			if !res.allowed {
				w.Header().Set("Retry-After", reset)
				httputil.HandleError(w, httputil.NewError("too many requests", nil, http.StatusTooManyRequests))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// formatSeconds rounds up to whole seconds, at least 1.
func formatSeconds(d time.Duration) string {
	s := int64((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}

	return strconv.FormatInt(s, 10)
}
