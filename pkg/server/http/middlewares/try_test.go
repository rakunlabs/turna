package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
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
		next    echo.HandlerFunc
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
			next: func(c echo.Context) error {
				path := c.Request().URL.Path
				// fmt.Println(path)

				if path == "/test/one" {
					c.Response().Flush()
					return c.String(http.StatusNotFound, http.StatusText(http.StatusNotFound))
				}

				return c.String(http.StatusOK, "Hello World")
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

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			got, err := m.Middleware()
			if (err != nil) != tt.wantErr {
				t.Errorf("Try.Middleware() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err := got(tt.next)(c); err != nil {
				t.Errorf("Try.Middleware() error = %v", err)
			}

			if rec.Body.String() != tt.want {
				t.Errorf("Try.Middleware() = %s, want %s", rec.Body.String(), tt.want)
			}
		})
	}
}
