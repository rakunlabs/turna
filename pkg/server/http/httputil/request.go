package httputil

import (
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/rakunlabs/query"
)

func IsWebSocket(r *http.Request) bool {
	upgrade := r.Header.Get(HeaderUpgrade)

	return strings.EqualFold(upgrade, "websocket")
}

func RealIP(r *http.Request) string {
	// TODO request context to get IPEXtractor
	// if c.echo != nil && c.echo.IPExtractor != nil {
	// 	return c.echo.IPExtractor(c.request)
	// }

	// Fall back to legacy behavior
	if ip := r.Header.Get(HeaderXForwardedFor); ip != "" {
		if i := strings.IndexByte(ip, ','); i >= 0 {
			ip = ip[:i]
		}

		ip = strings.TrimSpace(ip)
		ip = strings.TrimPrefix(ip, "[")
		ip = strings.TrimSuffix(ip, "]")
		if ip != "" {
			return ip
		}
	}
	if ip := r.Header.Get(HeaderXRealIP); ip != "" {
		ip = strings.TrimPrefix(ip, "[")
		ip = strings.TrimSuffix(ip, "]")
		return ip
	}
	ra, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ra
}

func Scheme(r *http.Request) string {
	// Can't use `r.Request.URL.Scheme`
	// See: https://groups.google.com/forum/#!topic/golang-nuts/pMUkBlQBDF0
	if r.TLS != nil {
		return "https"
	}
	if scheme := r.Header.Get(HeaderXForwardedProto); scheme != "" {
		return scheme
	}
	if scheme := r.Header.Get(HeaderXForwardedProtocol); scheme != "" {
		return scheme
	}
	if ssl := r.Header.Get(HeaderXForwardedSsl); ssl == "on" {
		return "https"
	}
	if scheme := r.Header.Get(HeaderXUrlScheme); scheme != "" {
		return scheme
	}
	return "http"
}

func QueryParam(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

func CommaQueryParam(v []string) []string {
	var r []string
	for _, s := range v {
		r = append(r, strings.Split(s, ",")...)
	}

	return r
}

func OffsetLimit(r *http.Request) (offset, limit int64) {
	offset, limit = 0, 20

	if v := r.URL.Query().Get("offset"); v != "" {
		vInt, _ := strconv.ParseInt(v, 10, 64)
		offset = vInt
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		vInt, _ := strconv.ParseInt(v, 10, 64)
		limit = vInt
	}

	return
}

func ParseQuery(r *http.Request, opts ...query.OptionQuery) (*query.Query, error) {
	return query.Parse(r.URL.RawQuery, opts...)
}

// CleanPath returns p with dot segments and repeated slashes removed,
// keeping a trailing slash. Access rules must match on it, an upstream
// normalizing /public/../admin would serve /admin otherwise.
func CleanPath(p string) string {
	if p == "" {
		return "/"
	}

	c := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}

	return c
}
