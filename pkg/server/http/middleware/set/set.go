package set

import (
	"net/http"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/tcontext"
)

// Set to set flag.
//
// Usable for other middlewares.
type Set struct {
	Values []string               `cfg:"values"`
	Map    map[string]interface{} `cfg:"map"`
}

func (s *Set) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			turna, ok := tcontext.GetTurna(r)
			if !ok {
				httputil.HandleError(w, httputil.NewError("turna not found", nil, http.StatusInternalServerError))

				return
			}

			for _, v := range s.Values {
				turna.Set(v, true)
			}

			for k, v := range s.Map {
				turna.Set(k, v)
			}

			next.ServeHTTP(w, r)
		})
	}
}
