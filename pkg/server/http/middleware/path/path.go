package path

import (
	"net/http"
)

type Path struct {
	Path string `cfg:"path"`

	Headers map[string]string `cfg:"headers"`
}

func (m *Path) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = m.Path

			for k, v := range m.Headers {
				if v == "" {
					r.Header.Del(k)

					continue
				}

				r.Header.Set(k, v)
			}

			next.ServeHTTP(w, r)
		})
	}
}
