package httputil

import (
	"errors"
	"net/http"
)

var ErrInvalidRedirectCode = errors.New("invalid redirect code")

const (
	HeaderLocation = "Location"
)

func Redirect(w http.ResponseWriter, code int, url string) error {
	if code < 300 || code > 308 {
		return ErrInvalidRedirectCode
	}

	w.Header().Set(HeaderLocation, url)
	w.WriteHeader(code)

	return nil
}
