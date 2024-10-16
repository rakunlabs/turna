package requestid

import (
	"net/http"

	"github.com/oklog/ulid/v2"
)

type RequestID struct {
	RequestIDResponse bool `cfg:"request_id_response"`
}

func (m RequestID) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-Id")
			if requestID == "" {
				requestID = ulid.Make().String()
			}

			r.Header.Set("X-Request-Id", requestID)
			if m.RequestIDResponse {
				w.Header().Set("X-Request-Id", requestID)
			}

			next.ServeHTTP(w, r)
		})
	}
}
