package block

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/chu/utils/decoder"
)

func TestBlock(t *testing.T) {
	m := &Block{
		Methods:   []string{"delete"},
		RegexPath: []string{`^/admin`, `\.env$`},
	}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api", http.StatusNoContent},
		{http.MethodDelete, "/api", http.StatusForbidden},
		{http.MethodGet, "/admin/x", http.StatusForbidden},
		{http.MethodGet, "/app/.env", http.StatusForbidden},
	}

	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

		if rec.Code != tt.want {
			t.Fatalf("%s %s: status = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
		}
	}
}

func TestBlockStatusMessage(t *testing.T) {
	mw, err := (&Block{RegexPath: []string{"^/"}, StatusCode: http.StatusNotFound, Message: "nope"}).Middleware()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mw(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "nope") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

// A single string keeps working for existing configs.
func TestBlockDecodeString(t *testing.T) {
	var m Block
	if err := decoder.New().Decode(map[string]any{"regex_path": "^/admin"}, &m); err != nil {
		t.Fatal(err)
	}

	if len(m.RegexPath) != 1 || m.RegexPath[0] != "^/admin" {
		t.Fatalf("regex_path = %#v", m.RegexPath)
	}
}

func TestBlockInvalidRegex(t *testing.T) {
	if _, err := (&Block{RegexPath: []string{"("}}).Middleware(); err == nil {
		t.Fatal("expected error")
	}
}
