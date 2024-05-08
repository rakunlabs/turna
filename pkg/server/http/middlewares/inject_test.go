package middlewares

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestInject_Middleware(t *testing.T) {
	type fields struct {
		PathMap map[string][]InjectContent `cfg:"path_map"`
		// ContentMap is the mime type to inject like "text/html"
		ContentMap map[string][]InjectContent `cfg:"content_map"`
	}
	tests := []struct {
		name   string
		fields fields
		path   string
		send   []byte
		want   []byte
	}{
		{
			name: "Test Inject Middleware",
			fields: fields{
				ContentMap: map[string][]InjectContent{
					"text/html": {
						{
							Old: "Hello World",
							New: "Hello Mars",
						},
					},
				},
			},
			path: "/",
			send: []byte("Hello World"),
			want: []byte("Hello Mars"),
		},
		{
			name: "Path and Regex",
			fields: fields{
				PathMap: map[string][]InjectContent{
					"/xyz/*": {
						{
							Regex: `(\s)`,
							New:   " Mars, ",
						},
					},
				},
			},
			path: "/xyz/2",
			send: []byte("Hello World"),
			want: []byte("Hello Mars, World"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Inject{
				ContentMap: tt.fields.ContentMap,
				PathMap:    tt.fields.PathMap,
			}
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			handler := func(c echo.Context) error {
				return c.Blob(http.StatusOK, "text/html", tt.send)
			}

			// Assert
			middleware, err := s.Middleware()
			if err != nil {
				t.Errorf("Inject.Middleware() error = %v", err)
			}

			if err := middleware[0](func(c echo.Context) error {
				return handler(c)
			})(c); err != nil {
				t.Errorf("Inject.Middleware() error = %v", err)
			}

			// Assert
			if got := rec.Body.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("Inject.Middleware() = %s, want %s", got, tt.want)
			}
		})
	}
}
