// Package ipfilter allows or denies HTTP requests by client IP.
package ipfilter

import (
	"errors"
	"fmt"
	"net/http"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

type IPFilter struct {
	// Allow lists IPs or CIDRs; when set, only matching clients pass.
	Allow []string `cfg:"allow"`
	// Deny lists IPs or CIDRs that are rejected, checked before Allow.
	Deny []string `cfg:"deny"`
	// TrustedProxies are CIDRs whose X-Forwarded-For / X-Real-IP headers are
	// used to find the client IP. Without it the connection address is used.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// StatusCode of rejected requests, default is 403.
	StatusCode int `cfg:"status_code"`
}

func (m *IPFilter) Middleware() (func(http.Handler) http.Handler, error) {
	if len(m.Allow) == 0 && len(m.Deny) == 0 {
		return nil, errors.New("ip_filter needs at least one allow or deny entry")
	}

	policy, err := adaproxy.New(m.TrustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("ip_filter trusted_proxies: %w", err)
	}

	var allow, deny *ipcheck.Checker
	if len(m.Allow) > 0 {
		if allow, err = ipcheck.NewChecker(m.Allow); err != nil {
			return nil, fmt.Errorf("ip_filter allow: %w", err)
		}
	}
	if len(m.Deny) > 0 {
		if deny, err = ipcheck.NewChecker(m.Deny); err != nil {
			return nil, fmt.Errorf("ip_filter deny: %w", err)
		}
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusForbidden
	}

	reject := func(w http.ResponseWriter) {
		httputil.HandleError(w, httputil.NewError("ip not allowed", nil, status))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, err := policy.ClientIP(r)
			if err != nil {
				// a trusted proxy sent malformed forwarding headers; falling
				// back to the proxy address could match an internal range
				reject(w)

				return
			}

			// non IP transports (unix sockets) match no range
			if deny != nil {
				if ok, _ := deny.Contains(ip); ok {
					reject(w)

					return
				}
			}

			if allow != nil {
				if ok, _ := allow.Contains(ip); !ok {
					reject(w)

					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
