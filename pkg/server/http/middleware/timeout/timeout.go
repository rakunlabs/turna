// Package timeout bounds the time spent on a request.
package timeout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Timeout struct {
	// Duration is the default request timeout, 0 disables it.
	Duration time.Duration `cfg:"duration"`
	// Rules override Duration for path prefixes, the first match wins.
	Rules []Rule `cfg:"rules"`
}

type Rule struct {
	Prefix []string `cfg:"prefix"`
	// Duration of matching requests, 0 disables the timeout.
	Duration time.Duration `cfg:"duration"`
}

func (m *Timeout) duration(path string) time.Duration {
	for _, rule := range m.Rules {
		for _, p := range rule.Prefix {
			if strings.HasPrefix(path, p) {
				return rule.Duration
			}
		}
	}

	return m.Duration
}

func (m *Timeout) Middleware() (func(http.Handler) http.Handler, error) {
	if m.Duration < 0 {
		return nil, errors.New("timeout duration must not be negative")
	}

	if m.Duration == 0 && len(m.Rules) == 0 {
		return nil, errors.New("timeout needs a duration or rules")
	}

	for i, rule := range m.Rules {
		if len(rule.Prefix) == 0 {
			return nil, fmt.Errorf("timeout rule %d has no prefix", i)
		}

		if rule.Duration < 0 {
			return nil, fmt.Errorf("timeout rule %d duration must not be negative", i)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d := m.duration(r.URL.Path)

			// upgraded connections are long lived and need the raw writer
			if d == 0 || httputil.IsWebSocket(r) {
				next.ServeHTTP(w, r)

				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			tw := &timeoutWriter{ResponseWriter: w, ctx: ctx, req: r}
			next.ServeHTTP(tw, r.WithContext(ctx))

			if !tw.written && tw.expired() {
				tw.timeout()
			}
		})
	}, nil
}

// timeoutWriter replaces a response started after the deadline (usually a
// proxy error) with 504.
type timeoutWriter struct {
	http.ResponseWriter

	ctx      context.Context
	req      *http.Request
	written  bool
	timedOut bool
}

func (w *timeoutWriter) expired() bool {
	return errors.Is(w.ctx.Err(), context.DeadlineExceeded)
}

func (w *timeoutWriter) timeout() {
	w.written = true
	w.timedOut = true

	if strings.HasPrefix(w.req.Header.Get("Content-Type"), "application/grpc") {
		h := w.ResponseWriter.Header()
		h.Set("Content-Type", "application/grpc")
		h.Set("Grpc-Status", "4") // DEADLINE_EXCEEDED
		h.Set("Grpc-Message", "request timeout")
		w.ResponseWriter.WriteHeader(http.StatusOK)

		return
	}

	httputil.HandleError(w.ResponseWriter, httputil.NewError("request timeout", nil, http.StatusGatewayTimeout))
}

func (w *timeoutWriter) WriteHeader(code int) {
	// 1xx responses are followed by the final one, 101 is final
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		if !w.written && !w.expired() {
			w.ResponseWriter.WriteHeader(code)
		}

		return
	}

	if w.written {
		if !w.timedOut {
			w.ResponseWriter.WriteHeader(code)
		}

		return
	}

	if w.expired() {
		w.timeout()

		return
	}

	w.written = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *timeoutWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.timedOut {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *timeoutWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.timedOut {
		f.Flush()
	}
}

func (w *timeoutWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
