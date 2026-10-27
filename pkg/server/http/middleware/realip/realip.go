// Package realip replaces the connection address of a request with the
// client address sent by trusted proxies.
package realip

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

const headerTrueClientIP = "True-Client-IP"

type RealIP struct {
	// TrustedProxies are IPs or CIDRs of proxies in front of Turna whose
	// forwarding headers are used.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// Header reads the client IP from a single header set by the trusted
	// proxy, such as CF-Connecting-IP, instead of X-Forwarded-For.
	Header string `cfg:"header"`
}

func (m *RealIP) Middleware() (func(http.Handler) http.Handler, error) {
	if len(m.TrustedProxies) == 0 {
		return nil, errors.New("real_ip trusted_proxies is required")
	}

	trusted, err := ipcheck.NewChecker(m.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("real_ip trusted_proxies: %w", err)
	}

	isTrusted := func(addr netip.Addr) bool {
		ok, _ := trusted.Contains(addr.String())

		return ok
	}

	header := http.CanonicalHeaderKey(strings.TrimSpace(m.Header))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, port, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// unix sockets and other non IP transports
				next.ServeHTTP(w, r)

				return
			}

			peer, err := parseAddr(host)
			if err != nil {
				next.ServeHTTP(w, r)

				return
			}

			if !isTrusted(peer) {
				// headers of an untrusted peer are forged
				r.Header.Del(httputil.HeaderXForwardedFor)
				r.Header.Del(httputil.HeaderXRealIP)
				r.Header.Del(headerTrueClientIP)
				if header != "" {
					r.Header.Del(header)
				}

				r.Header.Set(httputil.HeaderXRealIP, peer.String())
				next.ServeHTTP(w, r)

				return
			}

			client, rest, err := resolve(r.Header, header, isTrusted)
			if err != nil {
				httputil.HandleError(w, httputil.NewError("invalid forwarding header", err, http.StatusBadRequest))

				return
			}

			if !client.IsValid() {
				// the proxy sent no forwarding header, it is the client
				client = peer
			}

			r = r.Clone(r.Context())
			r.RemoteAddr = net.JoinHostPort(client.String(), port)

			// the reverse proxy appends RemoteAddr to X-Forwarded-For, so
			// keep only the hops before the client
			if len(rest) > 0 {
				r.Header.Set(httputil.HeaderXForwardedFor, strings.Join(rest, ", "))
			} else {
				r.Header.Del(httputil.HeaderXForwardedFor)
			}

			r.Header.Del(headerTrueClientIP)
			r.Header.Set(httputil.HeaderXRealIP, client.String())

			next.ServeHTTP(w, r)
		})
	}, nil
}

// resolve returns the client address and the X-Forwarded-For hops before it.
// The client is invalid when no forwarding header is set.
func resolve(h http.Header, header string, trusted func(netip.Addr) bool) (netip.Addr, []string, error) {
	if header != "" {
		raw, ok, err := single(h, header)
		if err != nil || !ok {
			return netip.Addr{}, nil, err
		}

		addr, err := parseAddr(raw)
		if err != nil {
			return netip.Addr{}, nil, fmt.Errorf("%s: %w", header, err)
		}

		return addr, nil, nil
	}

	if values := h.Values(httputil.HeaderXForwardedFor); len(values) > 0 {
		var hops []netip.Addr
		for _, v := range values {
			for _, raw := range strings.Split(v, ",") {
				addr, err := parseAddr(strings.TrimSpace(raw))
				if err != nil {
					return netip.Addr{}, nil, fmt.Errorf("%s: %w", httputil.HeaderXForwardedFor, err)
				}

				hops = append(hops, addr)
			}
		}

		// right to left, the first hop that is not a trusted proxy is the
		// client; when all are trusted the left most one is
		i := len(hops) - 1
		for i > 0 && trusted(hops[i]) {
			i--
		}

		rest := make([]string, 0, i)
		for _, hop := range hops[:i] {
			rest = append(rest, hop.String())
		}

		return hops[i], rest, nil
	}

	for _, name := range []string{headerTrueClientIP, httputil.HeaderXRealIP} {
		raw, ok, err := single(h, name)
		if err != nil {
			return netip.Addr{}, nil, err
		}

		if ok {
			addr, err := parseAddr(raw)
			if err != nil {
				return netip.Addr{}, nil, fmt.Errorf("%s: %w", name, err)
			}

			return addr, nil, nil
		}
	}

	return netip.Addr{}, nil, nil
}

func single(h http.Header, name string) (string, bool, error) {
	values := h.Values(name)
	if len(values) == 0 {
		return "", false, nil
	}

	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", true, fmt.Errorf("%s must have a single value", name)
	}

	return strings.TrimSpace(values[0]), true, nil
}

func parseAddr(raw string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("invalid IP %q", raw)
	}

	return addr.Unmap(), nil
}
