package decompress

import (
	"compress/gzip"
	"io"
	"net/http"
	"sync"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

type Decompress struct{}

func (m *Decompress) Middleware() func(http.Handler) http.Handler {
	pool := sync.Pool{New: func() interface{} { return new(gzip.Reader) }}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(httputil.HeaderContentEncoding) != "gzip" {
				next.ServeHTTP(w, r)

				return
			}

			i := pool.Get()
			gr, ok := i.(*gzip.Reader)
			if !ok || gr == nil {
				httputil.HandleError(w, httputil.NewError("", i.(error), http.StatusInternalServerError))

				return
			}
			defer pool.Put(gr)

			b := r.Body
			defer b.Close()

			if err := gr.Reset(b); err != nil {
				if err == io.EOF { //ignore if body is empty
					next.ServeHTTP(w, r)

					return
				}

				httputil.HandleError(w, httputil.NewError("", err, http.StatusInternalServerError))

				return
			}

			// only Close gzip reader if it was set to a proper gzip source otherwise it will panic on close.
			defer gr.Close()

			r.Body = gr

			next.ServeHTTP(w, r)
		})
	}
}
