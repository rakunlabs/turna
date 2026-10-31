// Package retry sends a request again when the upstream fails.
package retry

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/replay"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/statuscode"
)

var idempotentMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodDelete,
}

type Retry struct {
	// Attempts is the number of retries after the first request, default 2.
	Attempts *int `cfg:"attempts"`
	// Statuses that are retried, default 502, 503 and 504. Connection errors
	// and timeouts of the upstream are always retried.
	Statuses []string `cfg:"statuses"`
	// Methods that are retried, default the idempotent ones: GET, HEAD,
	// OPTIONS, TRACE, PUT and DELETE. Requests with an Idempotency-Key are
	// retried for any method.
	Methods []string `cfg:"methods"`

	// InitialInterval is the wait before the first retry, default 100ms.
	// It doubles for each retry up to MaxInterval, with jitter.
	InitialInterval time.Duration `cfg:"initial_interval"`
	// MaxInterval caps the wait between attempts, default 2s.
	MaxInterval time.Duration `cfg:"max_interval"`
	// RespectRetryAfter waits as long as a Retry-After response header asks,
	// up to MaxInterval.
	RespectRetryAfter bool `cfg:"respect_retry_after"`

	// MaxBodySize is the largest request body buffered to be sent again,
	// default 1MB. Requests with larger bodies are not retried.
	MaxBodySize int64 `cfg:"max_body_size"`
	// AttemptHeader is set on the request with the attempt number starting
	// at 1, e.g. X-Retry-Attempt.
	AttemptHeader string `cfg:"attempt_header"`
}

func (m *Retry) Middleware() (func(http.Handler) http.Handler, error) {
	attempts := 2
	if m.Attempts != nil {
		attempts = *m.Attempts
	}

	if attempts < 0 {
		return nil, errors.New("retry attempts must not be negative")
	}

	statuses, err := statuscode.Parse("retry", m.Statuses, "502-504")
	if err != nil {
		return nil, err
	}

	methods := idempotentMethods
	if len(m.Methods) > 0 {
		methods = nil
		for _, v := range m.Methods {
			methods = append(methods, strings.ToUpper(strings.TrimSpace(v)))
		}
	}

	initial := m.InitialInterval
	if initial == 0 {
		initial = 100 * time.Millisecond
	}

	maxInterval := m.MaxInterval
	if maxInterval == 0 {
		maxInterval = 2 * time.Second
	}

	if initial < 0 || maxInterval < 0 {
		return nil, errors.New("retry intervals must not be negative")
	}

	maxBody := m.MaxBodySize
	if maxBody <= 0 {
		maxBody = 1 << 20
	}

	attemptHeader := http.CanonicalHeaderKey(strings.TrimSpace(m.AttemptHeader))

	backoff := func(retry int, header http.Header) time.Duration {
		if m.RespectRetryAfter {
			if d, ok := retryAfter(header.Get("Retry-After")); ok {
				return min(d, maxInterval)
			}
		}

		d := initial << min(retry, 30)
		if d <= 0 || d > maxInterval {
			d = maxInterval
		}

		// equal jitter: half fixed, half random
		half := d / 2

		return half + rand.N(half+1) //nolint:gosec // jitter does not need crypto
	}

	return func(next http.Handler) http.Handler {
		if attempts == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if httputil.IsWebSocket(r) || (!slices.Contains(methods, r.Method) && r.Header.Get("Idempotency-Key") == "") {
				next.ServeHTTP(w, r)

				return
			}

			body, ok, err := replay.BufferBody(r, maxBody)
			if err != nil {
				httputil.HandleError(w, httputil.NewError("cannot read body", err, http.StatusBadRequest))

				return
			}

			if !ok {
				next.ServeHTTP(w, r)

				return
			}

			for attempt := 0; ; attempt++ {
				last := attempt >= attempts

				req, failed := httputil.WithUpstreamFailure(r.Clone(r.Context()))
				replay.SetBody(req, body)

				if attemptHeader != "" {
					req.Header.Set(attemptHeader, strconv.Itoa(attempt+1))
				}

				rw := replay.NewWriter(w, func(code int) bool {
					return !last && r.Context().Err() == nil && (statuses.Contains(code) || failed.Load())
				})

				next.ServeHTTP(rw, req)
				rw.Finish()

				if !rw.Discarded() {
					return
				}

				t := time.NewTimer(backoff(attempt, rw.Header()))
				select {
				case <-t.C:
				case <-r.Context().Done():
					t.Stop()

					return
				}
			}
		})
	}, nil
}

func retryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}

	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}

	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0), true
	}

	return 0, false
}
