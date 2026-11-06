// Package badbehavior bans clients that produce many error responses, e.g.
// scanners probing for paths.
package badbehavior

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/statuscode"
	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

type BadBehavior struct {
	// StatusCodes counted as bad, codes or ranges like "500-599".
	StatusCodes []string `cfg:"status_codes"`
	// Threshold of bad responses in Window that bans the client.
	Threshold int           `cfg:"threshold" default:"30"`
	Window    time.Duration `cfg:"window" default:"1m"`
	// BanTime is how long a client stays banned.
	BanTime time.Duration `cfg:"ban_time" default:"24h"`
	// StatusCode of requests from banned clients, default is 403.
	StatusCode int `cfg:"status_code"`

	// Allow lists IPs or CIDRs that are never counted or banned.
	Allow []string `cfg:"allow"`
	// TrustedProxies are CIDRs whose forwarding headers are used to find the
	// client IP.
	TrustedProxies []string `cfg:"trusted_proxies"`

	// Redis shares counters and bans between instances; default is in memory.
	Redis *Redis `cfg:"redis"`
	// FailClosed rejects requests with 503 when Redis is unavailable,
	// default lets them through.
	FailClosed bool `cfg:"fail_closed"`
}

var defaultStatusCodes = []string{"400", "401", "403", "404", "405", "429", "444"}

func (m *BadBehavior) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	// the config is fingerprinted to detect changes, defaults stay local
	threshold := m.Threshold
	if threshold == 0 {
		threshold = 30
	}

	window := m.Window
	if window == 0 {
		window = time.Minute
	}

	banTime := m.BanTime
	if banTime == 0 {
		banTime = 24 * time.Hour
	}

	if threshold < 0 {
		return nil, errors.New("bad_behavior threshold must be greater than 0")
	}

	if window < 0 {
		return nil, errors.New("bad_behavior window must be greater than 0")
	}

	if banTime < 0 {
		return nil, errors.New("bad_behavior ban_time must be greater than 0")
	}

	codes, err := statuscode.Parse("bad_behavior", m.StatusCodes, defaultStatusCodes...)
	if err != nil {
		return nil, err
	}

	policy, err := adaproxy.New(m.TrustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("bad_behavior trusted_proxies: %w", err)
	}

	var allow *ipcheck.Checker
	if len(m.Allow) > 0 {
		if allow, err = ipcheck.NewChecker(m.Allow); err != nil {
			return nil, fmt.Errorf("bad_behavior allow: %w", err)
		}
	}

	var s store
	if m.Redis != nil {
		if s, err = newRedisStore(ctx, m.Redis, threshold, window, banTime); err != nil {
			return nil, err
		}
	} else {
		s = newMemoryStore(ctx, threshold, window, banTime)
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusForbidden
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, err := policy.ClientIP(r)
			if err != nil {
				httputil.HandleError(w, httputil.NewError("ip not allowed", nil, status))

				return
			}

			if allow != nil {
				if ok, _ := allow.Contains(ip); ok {
					next.ServeHTTP(w, r)

					return
				}
			}

			remaining, err := s.banned(r.Context(), ip)
			if err != nil {
				if r.Context().Err() != nil {
					return
				}

				slog.Error("bad_behavior store failed", "error", err)

				if m.FailClosed {
					httputil.HandleError(w, httputil.NewError("bad behavior check unavailable", nil, http.StatusServiceUnavailable))

					return
				}
			}

			if remaining > 0 {
				w.Header().Set("Retry-After", formatSeconds(remaining))
				httputil.HandleError(w, httputil.NewError("client is banned", nil, status))

				return
			}

			rw := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rw, r)

			if rw.code == 0 || !codes.Contains(rw.code) {
				return
			}

			// counted even when the client went away, scanners often do
			banned, err := s.hit(context.WithoutCancel(r.Context()), ip)
			if err != nil {
				slog.Error("bad_behavior store failed", "error", err)

				return
			}

			if banned {
				slog.Warn("bad_behavior banned client",
					slog.String("ip", ip),
					slog.Int("status", rw.code),
					slog.String("path", r.URL.Path),
					slog.Duration("ban_time", banTime),
				)
			}
		})
	}, nil
}

// statusRecorder keeps the final status code of the response.
type statusRecorder struct {
	http.ResponseWriter

	code int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.code == 0 && (code < 100 || code >= 200 || code == http.StatusSwitchingProtocols) {
		w.code = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Flush() {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack keeps upgraded connections working for handlers that check for
// http.Hijacker directly.
func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// formatSeconds rounds up to whole seconds, at least 1.
func formatSeconds(d time.Duration) string {
	s := int64((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}

	return strconv.FormatInt(s, 10)
}
