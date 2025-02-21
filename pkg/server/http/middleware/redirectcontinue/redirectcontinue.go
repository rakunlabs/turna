package redirectcontinue

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

type RedirectionContinue struct {
	Redirects []Redirect `cfg:"redirects"`

	Permanent bool `cfg:"permanent"`
}

type Redirect struct {
	Regex       string `cfg:"regex"`
	Replacement string `cfg:"replacement"`

	rgx *regexp.Regexp
}

func (m *RedirectionContinue) Middleware() (func(http.Handler) http.Handler, error) {
	if len(m.Redirects) == 0 {
		return nil, fmt.Errorf("redirects is empty")
	}

	for i, r := range m.Redirects {
		if r.Regex == "" {
			return nil, fmt.Errorf("redirects[%d].regex is empty", i)
		}

		rgx, err := regexp.Compile(r.Regex)
		if err != nil {
			return nil, fmt.Errorf("redirects[%d].regex invalid: %w", i, err)
		}

		m.Redirects[i].rgx = rgx
	}

	statusCode := http.StatusTemporaryRedirect
	if m.Permanent {
		statusCode = http.StatusPermanentRedirect
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var u url.URL
			u.Path = r.URL.Path
			u.RawQuery = r.URL.RawQuery
			u.RawFragment = r.URL.RawFragment

			oldPath := u.String()

			for _, r := range m.Redirects {
				newPath := r.rgx.ReplaceAllString(oldPath, r.Replacement)

				if oldPath != newPath {
					httputil.Redirect(w, statusCode, newPath)

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
