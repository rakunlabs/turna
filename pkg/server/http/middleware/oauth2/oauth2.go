package oauth2

import (
	"net/http"
)

type Oauth2 struct {
}

func (m *Oauth2) Middleware() (func(http.Handler) http.Handler, error) {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}, nil
}
