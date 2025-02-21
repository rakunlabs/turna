package addprefix

import (
	"net/http"
	"net/url"
)

type AddPrefix struct {
	Prefix string `cfg:"prefix"`
}

func (m *AddPrefix) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			urlPath, err := url.JoinPath(m.Prefix + r.URL.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)

				return
			}

			r.URL.Path = urlPath

			next.ServeHTTP(w, r)
		})
	}
}
