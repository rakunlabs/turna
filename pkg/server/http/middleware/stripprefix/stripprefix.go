package stripprefix

import (
	"net/http"
	"net/url"
	"strings"
)

type StripPrefix struct {
	// ForceSlash default is true
	ForceSlash *bool    `cfg:"force_slash"`
	Prefixes   []string `cfg:"prefixes"`
	Prefix     string   `cfg:"prefix"`
}

func (a *StripPrefix) Strip(urlPath string) (string, error) {
	prefixes := []string{a.Prefix}
	if len(a.Prefixes) > 0 {
		prefixes = a.Prefixes
	}

	// strip url path in prefixes
	for _, prefix := range prefixes {
		if ok := strings.HasPrefix(urlPath, prefix); ok {
			urlPath = strings.TrimPrefix(urlPath, prefix)

			break
		}
	}

	forceSlash := true
	if a.ForceSlash != nil {
		forceSlash = *a.ForceSlash
	}

	// force slash
	if forceSlash {
		slashedPath, err := url.JoinPath("/", urlPath)
		if err != nil {
			return "", err
		}

		urlPath = slashedPath
	}

	return urlPath, nil
}

func (a *StripPrefix) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			urlPath, err := a.Strip(r.URL.Path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			r.URL.Path = urlPath

			next.ServeHTTP(w, r)
		})
	}
}
