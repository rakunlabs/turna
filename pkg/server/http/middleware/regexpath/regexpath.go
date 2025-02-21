package regexpath

import (
	"fmt"
	"net/http"
	"regexp"
)

type RegexPath struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`
}

func (m *RegexPath) Middleware() (func(http.Handler) http.Handler, error) {
	rgx, err := regexp.Compile(m.Regex)
	if err != nil {
		return nil, fmt.Errorf("regexPath invalid regex: %s", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = rgx.ReplaceAllString(r.URL.Path, m.Replacement)

			next.ServeHTTP(w, r)
		})
	}, nil
}
