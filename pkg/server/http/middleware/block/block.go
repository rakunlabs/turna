package block

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Block struct {
	Methods   []string `cfg:"methods"`
	RegexPath string   `cfg:"regex_path"`
}

func (m *Block) Middleware() (func(http.Handler) http.Handler, error) {
	methodsSet := make(map[string]struct{}, len(m.Methods))
	for _, m := range m.Methods {
		methodsSet[strings.ToUpper(m)] = struct{}{}
	}

	var rgxPath *regexp.Regexp
	if m.RegexPath != "" {
		rgx, err := regexp.Compile(m.RegexPath)
		if err != nil {
			return nil, err
		}

		rgxPath = rgx
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := methodsSet[r.Method]; ok {
				httputil.HandleError(w, httputil.NewError("method not allowed", nil, http.StatusForbidden))

				return
			}

			if rgxPath != nil {
				if rgxPath.MatchString(r.URL.Path) {
					httputil.HandleError(w, httputil.NewError("path not allowed", nil, http.StatusForbidden))

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
