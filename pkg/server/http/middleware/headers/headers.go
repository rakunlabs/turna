package headers

import (
	"net/http"
)

// Headers is a middleware that allows to add custom headers to the request and response.
//
// Delete the header, set empty values.
type Headers struct {
	CustomRequestHeaders  map[string]string `cfg:"custom_request_headers"`
	CustomResponseHeaders map[string]string `cfg:"custom_response_headers"`
}

func (h *Headers) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range h.CustomRequestHeaders {
				if v == "" {
					r.Header.Del(k)

					continue
				}

				r.Header.Set(k, v)
			}
			for k, v := range h.CustomResponseHeaders {
				if v == "" {
					w.Header().Del(k)

					continue
				}

				w.Header().Set(k, v)
			}

			next.ServeHTTP(w, r)
		})
	}
}
