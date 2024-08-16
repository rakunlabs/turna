package httputil

import (
	"errors"
	"net/http"
	"net/url"
)

var ErrInvalidRedirectCode = errors.New("invalid redirect code")

func Redirect(w http.ResponseWriter, code int, url string) error {
	if code < 300 || code > 308 {
		return ErrInvalidRedirectCode
	}

	w.Header().Set(HeaderLocation, url)
	w.WriteHeader(code)

	return nil
}

func RewriteRequestURLTarget(req *http.Request, target *url.URL) {
	targetQuery := target.RawQuery
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path, req.URL.RawPath = target.Path, target.RawPath
	if targetQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
	}
}
