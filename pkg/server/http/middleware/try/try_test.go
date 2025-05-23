package try

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

func TestTry_Middleware(t *testing.T) {
	type fields struct {
		Regex       string
		Replacement string
		StatusCodes string
	}
	tests := []struct {
		name    string
		fields  fields
		path    string
		next    func(w http.ResponseWriter, r *http.Request)
		want    string
		wantErr bool
	}{
		{
			name: "one",
			fields: fields{
				Regex:       "/test/(.*)",
				Replacement: "/test/_next/$1",
				StatusCodes: "404",
			},
			path: "/test/one",
			next: func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path

				if path == "/test/one" {
					w.(http.Flusher).Flush()
					httputil.HandleError(w, httputil.NewError("", nil, http.StatusNotFound))

					return
				}

				httputil.Blob(w, http.StatusOK, "text/plain", []byte("Hello World"))

				return
			},
			want: "Hello World",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Try{
				Regex:       tt.fields.Regex,
				Replacement: tt.fields.Replacement,
				StatusCodes: tt.fields.StatusCodes,
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			got, err := m.Middleware()
			if (err != nil) != tt.wantErr {
				t.Errorf("Try.Middleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			got(http.HandlerFunc(tt.next)).ServeHTTP(rec, req)

			if rec.Body.String() != tt.want {
				t.Errorf("Try.Middleware() = %s, want %s", rec.Body.String(), tt.want)
			}
		})
	}
}
