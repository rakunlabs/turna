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

	// AppendRequestHeaders and AppendResponseHeaders add values without
	// replacing existing ones, e.g. for Vary.
	AppendRequestHeaders  map[string]string `cfg:"append_request_headers"`
	AppendResponseHeaders map[string]string `cfg:"append_response_headers"`
}

func (h *Headers) applyResponse(header http.Header) {
	for k, v := range h.CustomResponseHeaders {
		if v == "" {
			header.Del(k)

			continue
		}

		header.Set(k, v)
	}

	for k, v := range h.AppendResponseHeaders {
		header.Add(k, v)
	}
}

func (h *Headers) Middleware() func(http.Handler) http.Handler {
	hasResponse := len(h.CustomResponseHeaders) > 0 || len(h.AppendResponseHeaders) > 0

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range h.CustomRequestHeaders {
				if v == "" {
					r.Header.Del(k)

					continue
				}

				r.Header.Set(k, v)
			}

			for k, v := range h.AppendRequestHeaders {
				r.Header.Add(k, v)
			}

			if !hasResponse {
				next.ServeHTTP(w, r)

				return
			}

			// applied when the response starts, so headers of later
			// handlers and upstreams can be replaced or removed
			rw := &responseWriter{ResponseWriter: w, apply: h.applyResponse}
			next.ServeHTTP(rw, r)

			if !rw.applied {
				rw.applyOnce()
			}
		})
	}
}

type responseWriter struct {
	http.ResponseWriter

	apply   func(http.Header)
	applied bool
}

func (w *responseWriter) applyOnce() {
	if w.applied {
		return
	}

	w.applied = true
	w.apply(w.ResponseWriter.Header())
}

func (w *responseWriter) WriteHeader(code int) {
	// 1xx responses are followed by the final one, 101 is final
	if code < 100 || code >= 200 || code == http.StatusSwitchingProtocols {
		w.applyOnce()
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.applyOnce()

	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Flush() {
	w.applyOnce()

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
