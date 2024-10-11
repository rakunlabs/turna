package httputil

import "net/http"

func NewMiddlewareHandler(handlers []func(next http.Handler) http.Handler) http.Handler {
	var handler http.Handler

	for i := len(handlers) - 1; i >= 0; i-- {
		handler = handlers[i](handler)
	}

	return handler
}
