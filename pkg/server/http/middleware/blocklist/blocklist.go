// Package blocklist rejects requests by client IP, reverse DNS, user agent,
// path or DNSBL listing, with lists that can be loaded from remote URLs.
package blocklist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Blocklist struct {
	// Deny rejects matching requests.
	Deny Rules `cfg:"deny"`
	// Allow matches bypass every deny check.
	Allow Rules `cfg:"allow"`
	// DNSBL zones, e.g. "zen.spamhaus.org"; a listed client is rejected.
	DNSBL []string `cfg:"dnsbl"`

	// Refresh interval of the remote lists, default 1h.
	Refresh time.Duration `cfg:"refresh" default:"1h"`
	// DNSCache is how long reverse DNS and DNSBL results are kept, default 1h.
	DNSCache time.Duration `cfg:"dns_cache" default:"1h"`
	// DNSTimeout of a single lookup, default 2s.
	DNSTimeout time.Duration `cfg:"dns_timeout" default:"2s"`
	// DNSCacheSize is the maximum number of cached lookups, default 100000.
	DNSCacheSize int `cfg:"dns_cache_size" default:"100000"`
	// RDNSVerify only uses reverse DNS names that resolve back to the client
	// IP, default true.
	RDNSVerify *bool `cfg:"rdns_verify"`

	// TrustedProxies are CIDRs whose forwarding headers are used to find the
	// client IP.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// StatusCode of rejected requests, default is 403.
	StatusCode int `cfg:"status_code"`

	resolver resolver
}

type Rules struct {
	// IP lists IPs or CIDRs.
	IP []string `cfg:"ip"`
	// RDNS lists domain suffixes of the client reverse DNS name, e.g. ".shodan.io".
	RDNS []string `cfg:"rdns"`
	// UserAgent lists regular expressions matched against the User-Agent header.
	UserAgent []string `cfg:"user_agent"`
	// URI lists regular expressions matched against the request path.
	URI []string `cfg:"uri"`
	// URLs are remote lists with one entry per line.
	URLs []URL `cfg:"urls"`
}

type URL struct {
	// URL is an http(s) or file:// address, or a local path.
	URL string `cfg:"url"`
	// Type of the entries: ip, rdns, user_agent or uri.
	Type string `cfg:"type"`
}

func (r Rules) empty() bool {
	return len(r.IP) == 0 && len(r.RDNS) == 0 && len(r.UserAgent) == 0 && len(r.URI) == 0 && len(r.URLs) == 0
}

func (m *Blocklist) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if m.Deny.empty() && len(m.DNSBL) == 0 {
		return nil, errors.New("blocklist needs at least one deny rule or dnsbl zone")
	}

	if m.Refresh < 0 || m.DNSCache < 0 || m.DNSTimeout < 0 || m.DNSCacheSize < 0 {
		return nil, errors.New("blocklist durations and dns_cache_size must not be negative")
	}

	policy, err := adaproxy.New(m.TrustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("blocklist trusted_proxies: %w", err)
	}

	zones, err := normalizeZones(m.DNSBL)
	if err != nil {
		return nil, err
	}

	l, err := newLists(m.Allow, m.Deny)
	if err != nil {
		return nil, err
	}

	refresh := m.Refresh
	if refresh == 0 {
		refresh = time.Hour
	}

	l.load(ctx)
	if l.remote() {
		go l.run(ctx, refresh)
	}

	verify := m.RDNSVerify == nil || *m.RDNSVerify

	var dns *lookups
	if l.rdns() || len(zones) > 0 {
		res := m.resolver
		if res == nil {
			res = &netResolver{}
		}

		dns = newLookups(ctx, res, lookupsConfig{
			ttl:     orDefault(m.DNSCache, time.Hour),
			timeout: orDefault(m.DNSTimeout, 2*time.Second),
			size:    orDefault(m.DNSCacheSize, 100_000),
			verify:  verify,
			zones:   zones,
		})
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusForbidden
	}

	reject := func(w http.ResponseWriter) {
		httputil.HandleError(w, httputil.NewError("request blocked", nil, status))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ipStr, err := policy.ClientIP(r)
			if err != nil {
				reject(w)

				return
			}

			// non IP transports (unix sockets) only match user agent and uri
			ip, err := netip.ParseAddr(ipStr)
			if err == nil {
				ip = ip.Unmap()
			}

			if !l.check(r.Context(), r, ip, dns) {
				reject(w)

				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}

	return v
}
