package telemetry

import (
	"net/http"
	"strings"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"
	adatelemetry "github.com/rakunlabs/ada/middleware/telemetry"
)

// Telemetry records OpenTelemetry traces and metrics for HTTP requests with
// the global providers configured under the top level telemetry setting.
type Telemetry struct {
	// PublicEndpoint starts a new trace instead of continuing the incoming one.
	PublicEndpoint bool `cfg:"public_endpoint"`
	// SkipPaths are path prefixes not instrumented, e.g. /healthz.
	SkipPaths []string `cfg:"skip_paths"`
	// TrustedProxies are CIDRs whose X-Forwarded-For / X-Real-IP headers are
	// used for client.address.
	TrustedProxies []string `cfg:"trusted_proxies"`
}

func (m *Telemetry) Middleware() func(http.Handler) http.Handler {
	var opts []adatelemetry.Option

	if m.PublicEndpoint {
		opts = append(opts, adatelemetry.WithPublicEndpoint())
	}

	if len(m.SkipPaths) > 0 {
		skip := m.SkipPaths
		opts = append(opts, adatelemetry.WithFilter(func(r *http.Request) bool {
			for _, p := range skip {
				if strings.HasPrefix(r.URL.Path, p) {
					return false
				}
			}

			return true
		}))
	}

	if len(m.TrustedProxies) > 0 {
		opts = append(opts, adatelemetry.WithClientIP(adaproxy.TrustedRealIP(m.TrustedProxies...)))
	}

	return adatelemetry.Middleware(opts...)
}
