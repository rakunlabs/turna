// Package maintenance answers requests with a maintenance response while it
// is enabled.
package maintenance

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

type Maintenance struct {
	// Enabled turns maintenance on, e.g. through a reloaded configuration.
	Enabled bool `cfg:"enabled"`
	// File turns maintenance on while it exists.
	File string `cfg:"file"`
	// CheckInterval of File, default 1s.
	CheckInterval time.Duration `cfg:"check_interval"`

	// Allow lists IPs or CIDRs that still reach the application.
	Allow []string `cfg:"allow"`
	// TrustedProxies are CIDRs whose forwarding headers are used for Allow.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// AllowPaths are path prefixes that still reach the application, e.g.
	// health checks.
	AllowPaths []string `cfg:"allow_paths"`
	// BypassHeader lets requests with this header and BypassValue through.
	BypassHeader string `cfg:"bypass_header"`
	BypassValue  string `cfg:"bypass_value" log:"-"`

	// StatusCode of the response, default 503.
	StatusCode int `cfg:"status_code"`
	// RetryAfter sets the Retry-After header, 0 leaves it out.
	RetryAfter time.Duration `cfg:"retry_after"`
	// Message is the response body, default a short JSON message.
	Message string `cfg:"message"`
	// ContentType of Message, default application/json for the default
	// message and text/html when Message starts with "<", else text/plain.
	ContentType string `cfg:"content_type"`
}

func (m *Maintenance) Middleware() (func(http.Handler) http.Handler, error) {
	if m.CheckInterval < 0 || m.RetryAfter < 0 {
		return nil, errors.New("maintenance durations must not be negative")
	}

	if (m.BypassHeader == "") != (m.BypassValue == "") {
		return nil, errors.New("maintenance bypass_header and bypass_value must be set together")
	}

	policy, err := adaproxy.New(m.TrustedProxies...)
	if err != nil {
		return nil, fmt.Errorf("maintenance trusted_proxies: %w", err)
	}

	var allow *ipcheck.Checker
	if len(m.Allow) > 0 {
		if allow, err = ipcheck.NewChecker(m.Allow); err != nil {
			return nil, fmt.Errorf("maintenance allow: %w", err)
		}
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusServiceUnavailable
	}

	body, contentType := m.Message, m.ContentType
	if body == "" {
		body = `{"message":"service is under maintenance"}`
		if contentType == "" {
			contentType = "application/json; charset=utf-8"
		}
	}
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
		if strings.HasPrefix(strings.TrimSpace(body), "<") {
			contentType = "text/html; charset=utf-8"
		}
	}

	var retryAfter string
	if m.RetryAfter > 0 {
		retryAfter = strconv.Itoa(int(math.Ceil(m.RetryAfter.Seconds())))
	}

	enabled := func() bool { return m.Enabled }
	if m.File != "" {
		interval := m.CheckInterval
		if interval == 0 {
			interval = time.Second
		}

		enabled = fileCheck(m.File, interval, m.Enabled)
	}

	bypass := func(r *http.Request) bool {
		for _, p := range m.AllowPaths {
			if strings.HasPrefix(r.URL.Path, p) {
				return true
			}
		}

		if m.BypassHeader != "" &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get(m.BypassHeader)), []byte(m.BypassValue)) == 1 {
			return true
		}

		if allow != nil {
			if ip, err := policy.ClientIP(r); err == nil {
				if ok, _ := allow.Contains(ip); ok {
					return true
				}
			}
		}

		return false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled() || bypass(r) {
				next.ServeHTTP(w, r)

				return
			}

			h := w.Header()
			h.Set("Content-Type", contentType)
			h.Set("Cache-Control", "no-store")
			if retryAfter != "" {
				h.Set("Retry-After", retryAfter)
			}

			w.WriteHeader(status)

			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(body))
			}
		})
	}, nil
}

// fileCheck reports whether path exists, checked at most once per interval.
// always keeps maintenance on regardless of the file.
func fileCheck(path string, interval time.Duration, always bool) func() bool {
	var (
		mu      sync.Mutex
		checked time.Time
		exists  bool
	)

	return func() bool {
		if always {
			return true
		}

		mu.Lock()
		defer mu.Unlock()

		if now := time.Now(); now.Sub(checked) >= interval {
			_, err := os.Stat(path)
			exists = err == nil
			checked = now
		}

		return exists
	}
}
