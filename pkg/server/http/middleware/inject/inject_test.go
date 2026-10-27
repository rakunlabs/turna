package inject

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

func TestInject_Middleware(t *testing.T) {
	type fields struct {
		PathMap map[string][]InjectContent `cfg:"path_map"`
	}
	tests := []struct {
		name   string
		fields fields
		path   string
		send   []byte
		want   []byte
	}{
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
				PathMap: tt.fields.PathMap,
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler := func(w http.ResponseWriter, r *http.Request) {
				httputil.Blob(w, http.StatusOK, "text/html", tt.send)
			}

			// Assert
			middleware, err := s.Middleware()
			if err != nil {
				t.Errorf("Inject.Middleware() error = %v", err)
			}

			middleware(http.HandlerFunc(handler)).ServeHTTP(rec, req)

			// Assert
			if got := rec.Body.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("Inject.Middleware() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestInjectFilters(t *testing.T) {
	s := &Inject{
		PathMap:      map[string][]InjectContent{"/**": {{Old: "World", New: "Mars"}}},
		ContentTypes: []string{"text/*"},
		Statuses:     []int{http.StatusOK},
		MaxBodySize:  "20B",
	}

	mw, err := s.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		contentType string
		status      int
		body        string
		want        string
	}{
		{"rewritten", "text/html; charset=utf-8", http.StatusOK, "Hello World", "Hello Mars"},
		{"other type", "application/json", http.StatusOK, "Hello World", "Hello World"},
		{"other status", "text/html", http.StatusNotFound, "Hello World", "Hello World"},
		{"too large", "text/plain", http.StatusOK, "Hello World, Hello World", "Hello World, Hello World"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != tt.status || rec.Body.String() != tt.want {
				t.Fatalf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tt.status, tt.want)
			}
		})
	}

	if _, err := (&Inject{MaxBodySize: "nope"}).Middleware(); err == nil {
		t.Fatal("expected error")
	}
}
