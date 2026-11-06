package gateway

import (
	"net/http/httptest"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	for in, want := range map[string]string{
		"/admin":            "/admin",
		"//admin":           "/admin",
		"/a//b///c":         "/a/b/c",
		"/public/../admin":  "/admin",
		"/public/..//admin": "/admin",
	} {
		if got := normalizePath(httptest.NewRequest("GET", "http://x"+in, nil)).URL.Path; got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}
