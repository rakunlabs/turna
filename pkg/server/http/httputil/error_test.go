package httputil

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestNewErrorAsKeepsCode(t *testing.T) {
	for name, err := range map[string]error{
		"value":   NewError("", errors.New("x"), http.StatusBadGateway),
		"pointer": &Error{Err: errors.New("x"), Code: http.StatusBadGateway},
		"wrapped": fmt.Errorf("wrap: %w", Error{Code: http.StatusBadGateway}),
	} {
		if got := NewErrorAs(err).Code; got != http.StatusBadGateway {
			t.Errorf("%s: code = %d", name, got)
		}
	}

	if got := NewErrorAs(errors.New("plain")).Code; got != http.StatusInternalServerError {
		t.Errorf("plain: code = %d", got)
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "/",
		"/":                "/",
		"/public/../admin": "/admin",
		"//admin":          "/admin",
		"/a/./b/":          "/a/b/",
		"public/x":         "/public/x",
		"/../..":           "/",
	} {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}
