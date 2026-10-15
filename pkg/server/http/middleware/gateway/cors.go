package gateway

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// CORS answers preflight requests and adds CORS headers to responses, with
// the semantics of the Gateway API HTTPRoute CORS filter.
type CORS struct {
	// AllowOrigins are "<scheme>://<host>[:<port>]" origins; "*" in the host
	// matches any number of labels to its left, "*" alone any origin.
	AllowOrigins []string `cfg:"allow_origins"`
	// AllowMethods are the allowed methods, "*" allows every method.
	AllowMethods []string `cfg:"allow_methods"`
	// AllowHeaders are the allowed request headers, "*" allows every header.
	AllowHeaders []string `cfg:"allow_headers"`
	// ExposeHeaders are the response headers exposed to scripts.
	ExposeHeaders []string `cfg:"expose_headers"`
	// AllowCredentials sets Access-Control-Allow-Credentials. Wildcards are
	// then answered with the requested values, as browsers reject "*".
	AllowCredentials bool `cfg:"allow_credentials"`
	// MaxAge in seconds of a preflight result, default 5.
	MaxAge int `cfg:"max_age"`
}

const defaultCORSMaxAge = 5

func (c *CORS) middleware() func(http.Handler) http.Handler {
	maxAge := c.MaxAge
	if maxAge <= 0 {
		maxAge = defaultCORSMaxAge
	}

	anyMethod := slices.Contains(c.AllowMethods, "*")
	anyHeader := slices.Contains(c.AllowHeaders, "*")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)

				return
			}

			h := w.Header()
			h.Add("Vary", "Origin")

			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			allowed := c.originAllowed(origin)

			if !allowed {
				if preflight {
					// no CORS headers: the browser does not send the request
					w.WriteHeader(http.StatusNoContent)

					return
				}

				next.ServeHTTP(w, r)

				return
			}

			h.Set("Access-Control-Allow-Origin", origin)
			if c.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if !preflight {
				if len(c.ExposeHeaders) > 0 {
					h.Set("Access-Control-Expose-Headers", c.exposeHeaders())
				}

				next.ServeHTTP(w, r)

				return
			}

			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")

			method := r.Header.Get("Access-Control-Request-Method")
			switch {
			case anyMethod:
				// a wildcard is not honored by browsers with credentials
				h.Set("Access-Control-Allow-Methods", method)
			case len(c.AllowMethods) > 0:
				h.Set("Access-Control-Allow-Methods", strings.Join(c.AllowMethods, ", "))
			}

			requested := r.Header.Get("Access-Control-Request-Headers")
			switch {
			case anyHeader:
				if requested != "" {
					h.Set("Access-Control-Allow-Headers", requested)
				}
			case len(c.AllowHeaders) > 0:
				h.Set("Access-Control-Allow-Headers", strings.Join(c.AllowHeaders, ", "))
			}

			if len(c.ExposeHeaders) > 0 {
				h.Set("Access-Control-Expose-Headers", c.exposeHeaders())
			}

			h.Set("Access-Control-Max-Age", strconv.Itoa(maxAge))

			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// exposeHeaders lists the exposed headers; a wildcard is not honored by
// browsers with credentials, so it is dropped then.
func (c *CORS) exposeHeaders() string {
	if !c.AllowCredentials {
		return strings.Join(c.ExposeHeaders, ", ")
	}

	out := slices.DeleteFunc(slices.Clone(c.ExposeHeaders), func(h string) bool { return h == "*" })

	return strings.Join(out, ", ")
}

func (c *CORS) originAllowed(origin string) bool {
	scheme, host, port, ok := splitOrigin(origin)
	if !ok {
		return false
	}

	for _, allowed := range c.AllowOrigins {
		if allowed == "*" {
			return true
		}

		as, ah, ap, ok := splitOrigin(allowed)
		if !ok || as != scheme || ap != port {
			continue
		}

		if ah == host {
			return true
		}

		// "*" matches any number of labels to its left
		if suffix, ok := strings.CutPrefix(ah, "*"); ok && strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}

	return false
}

// splitOrigin returns the lower case scheme, host and port of an origin,
// with the default port of the scheme when it is omitted.
func splitOrigin(origin string) (scheme, host, port string, ok bool) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return "", "", "", false
	}

	scheme = strings.ToLower(u.Scheme)
	host = strings.ToLower(u.Hostname())
	port = u.Port()

	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", "", "", false
		}
	}

	return scheme, host, port, true
}
