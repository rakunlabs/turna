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

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
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

type keyPart func(r *http.Request) string

func (m *RateLimit) keyFunc() (func(r *http.Request) string, error) {
	policy, err := adaproxy.New(m.TrustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("rate_limit trusted_proxies: %w", err)
	}

	clientIP := func(r *http.Request) string {
		if ip, err := policy.ClientIP(r); err == nil {
			return ip
		}

		return adaproxy.RealIP(r)
	}

	keys := m.Key
	if len(keys) == 0 {
		switch strings.ToLower(strings.TrimSpace(m.LimitType)) {
		case "ip":
			keys = []string{"ip"}
		case "realip":
			keys = []string{"realip"}
		case "", "all":
			return func(*http.Request) string { return "*" }, nil
		default:
			return nil, fmt.Errorf("rate_limit limit_type %q is unknown, expected all, ip or realip", m.LimitType)
		}
	}

	parts := make([]keyPart, 0, len(keys))
	for _, k := range keys {
		kind, name, _ := strings.Cut(strings.TrimSpace(k), ":")
		kind = strings.ToLower(kind)

		if (kind == "header" || kind == "query" || kind == "cookie") == (name == "") {
			return nil, fmt.Errorf("rate_limit key %q is invalid", k)
		}

		var part keyPart
		switch kind {
		case "ip":
			part = clientIP
		case "realip":
			part = adaproxy.UnsafeRealIP
		case "path":
			part = func(r *http.Request) string { return r.URL.Path }
		case "host":
			part = func(r *http.Request) string { return r.Host }
		case "method":
			part = func(r *http.Request) string { return r.Method }
		case "header":
			part = func(r *http.Request) string { return r.Header.Get(name) }
		case "query":
			part = func(r *http.Request) string { return r.URL.Query().Get(name) }
		case "cookie":
			part = func(r *http.Request) string {
				c, err := r.Cookie(name)
				if err != nil {
					return ""
				}

				return c.Value
			}
		default:
			return nil, fmt.Errorf("rate_limit key %q is unknown", k)
		}

		parts = append(parts, part)
	}

	return func(r *http.Request) string {
		var b strings.Builder
		for i, part := range parts {
			v := part(r)
			if v == "" {
				// omitting a header must not escape the limit
				v = "ip=" + clientIP(r)
			}

			if i > 0 {
				b.WriteByte(0)
			}
			b.WriteString(strconv.Itoa(len(v)))
			b.WriteByte(':')
			b.WriteString(v)
		}

		return b.String()
	}, nil
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
