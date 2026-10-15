package gateway

import (
	"bufio"
	"net"
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
			// responses differ by origin, also for requests without one
			w.Header().Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)

				return
			}

			h := w.Header()

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

			if !preflight {
				// set when the final response is written: interim responses
				// clear the headers, and the filter replaces CORS headers of
				// the backend
				next.ServeHTTP(&corsWriter{ResponseWriter: w, set: func(h http.Header) { c.responseHeaders(h, origin) }}, r)

				return
			}

			c.responseHeaders(h, origin)

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

			h.Set("Access-Control-Max-Age", strconv.Itoa(maxAge))

			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// responseHeaders sets the CORS headers of a response to an allowed origin.
func (c *CORS) responseHeaders(h http.Header, origin string) {
	if !slices.Contains(h.Values("Vary"), "Origin") {
		h.Add("Vary", "Origin")
	}

	h.Set("Access-Control-Allow-Origin", origin)

	h.Del("Access-Control-Allow-Credentials")
	if c.AllowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}

	h.Del("Access-Control-Expose-Headers")
	if e := c.exposeHeaders(); e != "" {
		h.Set("Access-Control-Expose-Headers", e)
	}
}

// corsWriter sets the CORS headers when the final response starts.
type corsWriter struct {
	http.ResponseWriter

	set     func(http.Header)
	written bool
}

func (w *corsWriter) WriteHeader(code int) {
	if !w.written && !informational(code) {
		w.written = true
		w.set(w.Header())
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *corsWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(b)
}

func (w *corsWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *corsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.written {
		w.written = true
		w.set(w.Header())
	}

	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *corsWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
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
