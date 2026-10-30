package block

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Block struct {
	// All blocks every request.
	All     bool     `cfg:"all"`
	Methods []string `cfg:"methods"`
	// RegexPath blocks paths matching any of the regular expressions.
	RegexPath []string `cfg:"regex_path"`
	// StatusCode of blocked requests, default is 403.
	StatusCode int `cfg:"status_code"`
	// Message of the error body, default is "not allowed", "method not allowed" or "path not allowed".
	Message string `cfg:"message"`
}

func (m *Block) Middleware() (func(http.Handler) http.Handler, error) {
	methodsSet := make(map[string]struct{}, len(m.Methods))
	for _, m := range m.Methods {
		methodsSet[strings.ToUpper(m)] = struct{}{}
	}

	rgxPaths := make([]*regexp.Regexp, 0, len(m.RegexPath))
	for _, p := range m.RegexPath {
		rgx, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("block regex_path %q: %w", p, err)
		}

		rgxPaths = append(rgxPaths, rgx)
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusForbidden
	}

	reject := func(w http.ResponseWriter, msg string) {
		if m.Message != "" {
			msg = m.Message
		}

		httputil.HandleError(w, httputil.NewError(msg, nil, status))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.All {
				reject(w, "not allowed")

				return
			}

			if _, ok := methodsSet[r.Method]; ok {
				reject(w, "method not allowed")

				return
			}

			for _, rgx := range rgxPaths {
				if rgx.MatchString(r.URL.Path) {
					reject(w, "path not allowed")

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
