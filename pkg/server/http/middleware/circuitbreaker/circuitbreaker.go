// Package circuitbreaker stops sending requests to a failing upstream for a
// while and answers them directly instead.
package circuitbreaker

import (
	"bufio"
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/breaker"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

var defaultFailureStatuses = []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}

type CircuitBreaker struct {
	breaker.Config `cfg:",squash"`

	// FailureStatuses are response statuses counted as failures, default
	// 502, 503 and 504. Unreachable upstreams and timeouts always count.
	FailureStatuses []int `cfg:"failure_statuses"`
	// SlowThreshold counts responses slower than it as failures, 0 disables.
	SlowThreshold time.Duration `cfg:"slow_threshold"`
	// StatusCode of rejected requests, default is 503.
	StatusCode int `cfg:"status_code"`
}

func (m *CircuitBreaker) Middleware() (func(http.Handler) http.Handler, error) {
	b, err := breaker.New("circuit_breaker", m.Config)
	if err != nil {
		return nil, err
	}

	if m.SlowThreshold < 0 {
		return nil, errors.New("circuit_breaker slow_threshold must not be negative")
	}

	statuses := m.FailureStatuses
	if len(statuses) == 0 {
		statuses = defaultFailureStatuses
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusServiceUnavailable
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gen, ok, wait := b.Allow()
			if !ok {
				reject(w, r, status, wait)

				return
			}

			start := time.Now()
			req, unreachable := httputil.WithUpstreamFailure(r)
			sw := &statusWriter{ResponseWriter: w}

			reported := false
			defer func() {
				if reported {
					return
				}

				// the reverse proxy aborts with ErrAbortHandler when the
				// client goes away; other panics are failures
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value
						b.Release(gen)
					} else {
						b.Report(gen, true)
					}

					panic(v)
				}
			}()

			next.ServeHTTP(sw, req)

			reported = true

			if errors.Is(r.Context().Err(), context.Canceled) && !sw.hijacked {
				// the client went away, the upstream state is unknown
				b.Release(gen)

				return
			}

			code := sw.code
			if code == 0 {
				code = http.StatusOK
			}

			failed := unreachable.Load() ||
				slices.Contains(statuses, code) ||
				(m.SlowThreshold > 0 && time.Since(start) > m.SlowThreshold)

			b.Report(gen, failed)
		})
	}, nil
}

func reject(w http.ResponseWriter, r *http.Request, status int, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		h := w.Header()
		h.Set("Content-Type", "application/grpc")
		h.Set("Grpc-Status", "14") // UNAVAILABLE
		h.Set("Grpc-Message", "circuit breaker is open")
		w.WriteHeader(http.StatusOK)

		return
	}

	httputil.HandleError(w, httputil.NewError("circuit breaker is open", nil, status))
}

// statusWriter records the final response status.
type statusWriter struct {
	http.ResponseWriter

	code     int
	hijacked bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.code == 0 && (code < 100 || code >= 200 || code == http.StatusSwitchingProtocols) {
		w.code = code
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if w.code == 0 {
		w.code = http.StatusOK
	}

	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
		if w.code == 0 {
			w.code = http.StatusSwitchingProtocols
		}
	}

	return conn, rw, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
