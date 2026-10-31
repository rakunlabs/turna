package gateway

import (
	"net/http"
	"slices"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/replay"
)

// DefaultRetryMaxBodySize is the largest request body buffered for retries.
const DefaultRetryMaxBodySize = 1 << 20

// defaultRetryCodes are retried when Retry.Codes is empty.
var defaultRetryCodes = []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}

type Retry struct {
	// Codes are response status codes that are retried, default is 502, 503
	// and 504.
	Codes []int `cfg:"codes"`
	// Attempts is the number of retries after the first request, default 1;
	// 0 disables retries.
	Attempts *int `cfg:"attempts"`
	// Backoff is the minimum time between attempts.
	Backoff time.Duration `cfg:"backoff"`
	// MaxBodySize is the largest request body buffered to be sent again,
	// default 1MiB. Requests with larger bodies are not retried.
	MaxBodySize int64 `cfg:"max_body_size"`
}

// retry runs next again while it answers a retryable status, could not
// reach an upstream or timed out; connection errors and backend timeouts are
// retried regardless of Codes. Each attempt gets a fresh copy of the request
// and its own response headers; a retryable response is discarded before
// anything is written, so other responses stream unchanged.
func (c *Retry) middleware(backendTimeout time.Duration) func(http.Handler) http.Handler {
	codes := c.Codes
	if len(codes) == 0 {
		codes = defaultRetryCodes
	}

	attempts := 1
	if c.Attempts != nil {
		attempts = max(*c.Attempts, 0)
	}

	maxBody := c.MaxBodySize
	if maxBody <= 0 {
		maxBody = DefaultRetryMaxBodySize
	}

	return func(next http.Handler) http.Handler {
		if backendTimeout > 0 {
			next = timeout(backendTimeout)(next)
		}

		if attempts == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok, err := replay.BufferBody(r, maxBody)
			if err != nil {
				writeError(w, r, http.StatusBadRequest)

				return
			}

			if !ok {
				// body too large to replay
				next.ServeHTTP(w, r)

				return
			}

			for attempt := 0; ; attempt++ {
				if attempt > 0 && c.Backoff > 0 {
					t := time.NewTimer(c.Backoff)
					select {
					case <-t.C:
					case <-r.Context().Done():
						t.Stop()

						return
					}
				}

				last := attempt >= attempts

				req, failed := httputil.WithUpstreamFailure(r.Clone(r.Context()))
				rw := replay.NewWriter(w, func(code int) bool {
					return !last && r.Context().Err() == nil && (slices.Contains(codes, code) || failed.Load())
				})

				replay.SetBody(req, body)

				next.ServeHTTP(rw, req)
				rw.Finish()

				if !rw.Discarded() {
					return
				}
			}
		})
	}
}
