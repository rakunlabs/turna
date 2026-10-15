package gateway

import (
	"net"
	"net/http"
	"strings"
)

// misdirected reports whether the TLS server name and the Host header of r
// select different listeners.
func (m *Gateway) misdirected(r *http.Request) bool {
	if len(m.Listeners) < 2 || r.TLS == nil || r.TLS.ServerName == "" {
		return false
	}

	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	sni := normalizeHost(r.TLS.ServerName)

	return selectListener(m.Listeners, sni) != selectListener(m.Listeners, normalizeHost(host))
}

func normalizeHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

// selectListener returns the index of the listener serving host: the exact
// hostname, then the longest matching wildcard, then the listener without
// hostname; -1 when none matches.
func selectListener(listeners []string, host string) int {
	best, bestLen, catchAll := -1, 0, -1

	for i, l := range listeners {
		l = normalizeHost(l)

		switch {
		case l == "":
			if catchAll < 0 {
				catchAll = i
			}
		case l == host:
			return i
		case strings.HasPrefix(l, "*."):
			suffix := l[1:]
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) && len(suffix) > bestLen {
				best, bestLen = i, len(suffix)
			}
		}
	}

	if best >= 0 {
		return best
	}

	return catchAll
}
