package gateway

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

type weighted struct {
	handlers []http.Handler
	weights  []int
	total    int
}

func newWeighted(backends []Backend) (http.Handler, error) {
	w := &weighted{}

	for i, b := range backends {
		if b.Weight < 0 {
			return nil, fmt.Errorf("backend %d: weight must not be negative", i)
		}

		var handler http.Handler

		if b.Status != 0 {
			status := b.Status
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, r, status)
			})
		} else {
			named, err := resolve(b.Middlewares)
			if err != nil {
				return nil, fmt.Errorf("backend %d: %w", i, err)
			}

			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, r, http.StatusInternalServerError)
			})

			for j := len(named) - 1; j >= 0; j-- {
				handler = named[j](handler)
			}
		}

		w.handlers = append(w.handlers, handler)
		w.weights = append(w.weights, b.Weight)
		w.total += b.Weight
	}

	return w, nil
}

func (w *weighted) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if w.total == 0 {
		// all backends have zero weight
		writeError(rw, r, http.StatusInternalServerError)

		return
	}

	n := rand.IntN(w.total)
	for i, weight := range w.weights {
		if n < weight {
			w.handlers[i].ServeHTTP(rw, r)

			return
		}

		n -= weight
	}
}

func timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			tw := &timeoutWriter{ResponseWriter: w, ctx: ctx, req: r}
			next.ServeHTTP(tw, r.WithContext(ctx))

			if !tw.written && tw.expired() {
				tw.timeout()
			}
		})
	}
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
	writeError(w.ResponseWriter, w.req, http.StatusGatewayTimeout)
}

func (w *timeoutWriter) WriteHeader(code int) {
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

type nextKey struct{}

func withNext(r *http.Request, next http.Handler) *http.Request {
	if next == nil {
		return r
	}

	return r.WithContext(context.WithValue(r.Context(), nextKey{}, next))
}

func nextFrom(r *http.Request) http.Handler {
	next, _ := r.Context().Value(nextKey{}).(http.Handler)

	return next
}
