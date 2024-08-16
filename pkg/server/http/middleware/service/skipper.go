package service

import (
	"net/http"
)

type (
	// Skipper defines a function to skip middleware. Returning true skips processing
	// the middleware.
	Skipper func(w http.ResponseWriter, r *http.Request) bool
)

func DefaultSkipper(_ http.ResponseWriter, _ *http.Request) bool {
	return false
}
