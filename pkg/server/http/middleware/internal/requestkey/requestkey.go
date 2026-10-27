// Package requestkey builds keys of requests to count or limit them by.
package requestkey

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"
)

type keyPart func(r *http.Request) string

// New returns a function building a key of the request from parts:
//
//	ip, realip, path, host, method, header:<name>, query:<name>, cookie:<name>
//
// A part missing in the request falls back to the client IP so omitting it
// does not escape a limit. Without keys all requests share one key. mw prefixes configuration errors.
func New(mw string, keys, trustedProxies []string) (func(r *http.Request) string, error) {
	policy, err := adaproxy.New(trustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("%s trusted_proxies: %w", mw, err)
	}

	clientIP := func(r *http.Request) string {
		if ip, err := policy.ClientIP(r); err == nil {
			return ip
		}

		return adaproxy.RealIP(r)
	}

	if len(keys) == 0 {
		return func(*http.Request) string { return "*" }, nil
	}

	parts := make([]keyPart, 0, len(keys))
	for _, k := range keys {
		kind, name, _ := strings.Cut(strings.TrimSpace(k), ":")
		kind = strings.ToLower(kind)

		if (kind == "header" || kind == "query" || kind == "cookie") == (name == "") {
			return nil, fmt.Errorf("%s key %q is invalid", mw, k)
		}

		var part keyPart
		switch kind {
		case "ip":
			part = clientIP
		case "realip":
			part = adaproxy.UnsafeRealIP
		case "path":
			part = func(r *http.Request) string { return r.URL.Path }
		case "host":
			part = func(r *http.Request) string { return r.Host }
		case "method":
			part = func(r *http.Request) string { return r.Method }
		case "header":
			part = func(r *http.Request) string { return r.Header.Get(name) }
		case "query":
			part = func(r *http.Request) string { return r.URL.Query().Get(name) }
		case "cookie":
			part = func(r *http.Request) string {
				c, err := r.Cookie(name)
				if err != nil {
					return ""
				}

				return c.Value
			}
		default:
			return nil, fmt.Errorf("%s key %q is unknown", mw, k)
		}

		parts = append(parts, part)
	}

	return func(r *http.Request) string {
		var b strings.Builder
		for i, part := range parts {
			v := part(r)
			if v == "" {
				// omitting a header must not escape the limit
				v = "ip=" + clientIP(r)
			}

			if i > 0 {
				b.WriteByte(0)
			}
			b.WriteString(strconv.Itoa(len(v)))
			b.WriteByte(':')
			b.WriteString(v)
		}

		return b.String()
	}, nil
}
