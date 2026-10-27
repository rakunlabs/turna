package cache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type directives map[string]string

func parseCacheControl(h http.Header) directives {
	d := directives{}

	for _, v := range h.Values("Cache-Control") {
		for _, part := range strings.Split(v, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(part), "=")
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				d[name] = strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
	}

	return d
}

func (d directives) has(name string) bool {
	_, ok := d[name]

	return ok
}

// seconds returns the value of a delta-seconds directive.
func (d directives) seconds(name string) (time.Duration, bool) {
	v, ok := d[name]
	if !ok {
		return 0, false
	}

	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}

	return time.Duration(min(n, int64(1<<31))) * time.Second, true
}

// lifetime is how long a stored response can be used.
type lifetime struct {
	fresh time.Duration
	swr   time.Duration
	sie   time.Duration
}

func (l lifetime) total() time.Duration {
	return l.fresh + max(l.swr, l.sie)
}

// lifetime returns the lifetime of a response, ok is false when it must not
// be stored.
func (m *Cache) lifetime(r *http.Request, status int, h http.Header) (lifetime, bool) {
	if !m.statuses[status] {
		return lifetime{}, false
	}

	cc := parseCacheControl(h)
	if cc.has("no-store") || cc.has("private") || cc.has("no-cache") {
		return lifetime{}, false
	}

	if len(h.Values("Set-Cookie")) > 0 {
		return lifetime{}, false
	}

	for _, v := range h.Values("Vary") {
		if strings.Contains(v, "*") {
			return lifetime{}, false
		}
	}

	if r.Header.Get("Authorization") != "" && !m.CacheAuthorized &&
		!cc.has("public") && !cc.has("s-maxage") && !cc.has("must-revalidate") {
		return lifetime{}, false
	}

	var l lifetime

	if v, ok := cc.seconds("s-maxage"); ok {
		l.fresh = v
	} else if v, ok := cc.seconds("max-age"); ok {
		l.fresh = v
	} else if exp := h.Get("Expires"); exp != "" {
		t, err := http.ParseTime(exp)
		if err != nil {
			// an invalid Expires means already expired
			return lifetime{}, false
		}

		date := time.Now()
		if d, err := http.ParseTime(h.Get("Date")); err == nil {
			date = d
		}

		l.fresh = t.Sub(date)
	} else {
		l.fresh = m.TTL
	}

	if age, err := strconv.ParseInt(h.Get("Age"), 10, 64); err == nil && age > 0 {
		l.fresh -= time.Duration(age) * time.Second
	}

	if m.MaxTTL > 0 {
		l.fresh = min(l.fresh, m.MaxTTL)
	}

	l.fresh = max(l.fresh, 0)

	l.swr = m.StaleWhileRevalidate
	if v, ok := cc.seconds("stale-while-revalidate"); ok {
		l.swr = v
	}

	l.sie = m.StaleIfError
	if v, ok := cc.seconds("stale-if-error"); ok {
		l.sie = v
	}

	if cc.has("must-revalidate") || cc.has("proxy-revalidate") {
		l.swr, l.sie = 0, 0
	}

	if l.total() <= 0 {
		return lifetime{}, false
	}

	return l, true
}

// hop-by-hop and per response headers that are not stored.
var skipHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
	"Age":                 true,
	"Date":                true,
}
