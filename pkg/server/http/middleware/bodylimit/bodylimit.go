// Package bodylimit rejects request bodies larger than a configured size.
package bodylimit

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/dustin/go-humanize"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type BodyLimit struct {
	// MaxSize is the maximum request body size, e.g. 10MB, 512KiB or 1048576.
	MaxSize string `cfg:"max_size"`
}

func (m *BodyLimit) Middleware() (func(http.Handler) http.Handler, error) {
	if m.MaxSize == "" {
		return nil, errors.New("body_limit max_size is required")
	}

	size, err := humanize.ParseBytes(m.MaxSize)
	if err != nil {
		return nil, fmt.Errorf("body_limit max_size %q: %w", m.MaxSize, err)
	}

	if size == 0 || size > 1<<62 {
		return nil, fmt.Errorf("body_limit max_size %q is out of range", m.MaxSize)
	}

	limit := int64(size)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				httputil.HandleError(w, httputil.NewError(
					fmt.Sprintf("request body too large, limit is %d bytes", limit), nil, http.StatusRequestEntityTooLarge,
				))

				return
			}

			// chunked bodies have no length, reads fail past the limit
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
