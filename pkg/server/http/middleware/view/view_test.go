package view

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareServesUIAtPrefixBase(t *testing.T) {
	m := &View{PrefixPath: "/view"}
	middleware, err := m.Middleware(context.Background(), "view-test")
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}

	for _, tc := range []struct {
		path string
		code int
	}{
		{path: "/view", code: http.StatusMovedPermanently},
		{path: "/view/", code: http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		middleware(http.NotFoundHandler()).ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%s status = %d, want %d; body=%q", tc.path, rec.Code, tc.code, rec.Body.String())
		}
	}
}
