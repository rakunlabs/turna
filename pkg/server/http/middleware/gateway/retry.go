package gateway

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
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
			body, ok, err := bufferBody(r, maxBody)
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
				rw := &retryWriter{
					ResponseWriter: w,
					header:         w.Header().Clone(),
					retryable: func(code int) bool {
						return !last && r.Context().Err() == nil && (slices.Contains(codes, code) || failed.Load())
					},
				}

				if body != nil {
					req.Body = io.NopCloser(bytes.NewReader(body))
					req.GetBody = func() (io.ReadCloser, error) {
						return io.NopCloser(bytes.NewReader(body)), nil
					}
				}

				next.ServeHTTP(rw, req)

				if !rw.discard {
					return
				}
			}
		})
	}
}

// bufferBody reads the request body when it is not larger than limit. ok is
// false when the body is larger; r.Body then still yields the whole body.
func bufferBody(r *http.Request, limit int64) (body []byte, ok bool, err error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true, nil
	}

	if r.ContentLength > limit {
		return nil, false, nil
	}

	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, false, err
	}

	if int64(len(buf)) > limit {
		r.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(buf), r.Body), Closer: r.Body}

		return nil, false, nil
	}

	_ = r.Body.Close()

	return buf, true, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

// retryWriter holds back response headers until the status is known and
// drops the response when the status is retryable.
type retryWriter struct {
	http.ResponseWriter

	header    http.Header
	retryable func(code int) bool
	written   bool
	discard   bool
}

func (w *retryWriter) Header() http.Header {
	if w.written && !w.discard {
		// trailers are set after the status is written
		return w.ResponseWriter.Header()
	}

	return w.header
}

func (w *retryWriter) WriteHeader(code int) {
	if w.written {
		return
	}

	if informational(code) {
		return
	}

	w.written = true

	if w.retryable(code) {
		w.discard = true

		return
	}

	dst := w.ResponseWriter.Header()
	for k := range dst {
		if _, ok := w.header[k]; !ok {
			delete(dst, k)
		}
	}

	for k, v := range w.header {
		dst[k] = v
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *retryWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.discard {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *retryWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if w.discard {
		return
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hands the connection over, for example for upgraded connections.
// A hijacked attempt is final.
func (w *retryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.written = true

	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *retryWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
