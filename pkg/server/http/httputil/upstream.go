package httputil

import (
	"context"
	"net/http"
	"sync/atomic"
)

type upstreamFailureKey struct{}

// WithUpstreamFailure returns a request that records whether the upstream
// could not be reached or timed out, as opposed to answering with an error
// status. Retries use it to retry connection errors and timeouts regardless
// of the retried status codes.
func WithUpstreamFailure(r *http.Request) (*http.Request, *atomic.Bool) {
	failed := &atomic.Bool{}

	return r.WithContext(context.WithValue(r.Context(), upstreamFailureKey{}, failed)), failed
}

// ReportUpstreamFailure marks the request of WithUpstreamFailure as failed
// without an upstream response.
func ReportUpstreamFailure(r *http.Request) {
	if failed, ok := r.Context().Value(upstreamFailureKey{}).(*atomic.Bool); ok {
		failed.Store(true)
	}
}
