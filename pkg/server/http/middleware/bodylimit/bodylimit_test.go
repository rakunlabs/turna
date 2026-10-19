package bodylimit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func handler(t *testing.T, maxSize string) http.Handler {
	t.Helper()

	mw, err := (&BodyLimit{MaxSize: maxSize}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
}

func TestBodyLimit(t *testing.T) {
	h := handler(t, "10B")

	tests := []struct {
		name    string
		body    string
		chunked bool
		want    int
	}{
		{"under", "hello", false, http.StatusNoContent},
		{"exact", "0123456789", false, http.StatusNoContent},
		{"over content length", "0123456789a", false, http.StatusRequestEntityTooLarge},
		{"over chunked", "0123456789a", true, http.StatusRequestEntityTooLarge},
		{"under chunked", "hi", true, http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.chunked {
				req.ContentLength = -1
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestBodyLimitNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(t, "1B").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBodyLimitConfig(t *testing.T) {
	for _, v := range []string{"", "abc", "0"} {
		if _, err := (&BodyLimit{MaxSize: v}).Middleware(); err == nil {
			t.Fatalf("%q: expected error", v)
		}
	}

	for _, v := range []string{"1MB", "512KiB", "1048576"} {
		if _, err := (&BodyLimit{MaxSize: v}).Middleware(); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
}
