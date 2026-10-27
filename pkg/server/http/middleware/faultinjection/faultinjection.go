// Package faultinjection delays or fails a share of requests to test how
// clients handle slow and failing services.
package faultinjection

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type FaultInjection struct {
	// Delay slows requests down before they continue.
	Delay *Delay `cfg:"delay"`
	// Abort answers requests with an error instead of continuing.
	Abort *Abort `cfg:"abort"`

	// Header limits faults to requests with this header, e.g.
	// X-Fault-Inject: true. A request can set its own percent with a number.
	Header string `cfg:"header"`
	// Paths limits faults to these path prefixes.
	Paths []string `cfg:"paths"`
}

type Delay struct {
	Duration time.Duration `cfg:"duration"`
	// Jitter adds a random duration up to it.
	Jitter time.Duration `cfg:"jitter"`
	// Percent of requests delayed, 0-100.
	Percent float64 `cfg:"percent"`
}

type Abort struct {
	// StatusCode of aborted requests, default 503.
	StatusCode int `cfg:"status_code"`
	// Percent of requests aborted, 0-100.
	Percent float64 `cfg:"percent"`
}

func checkPercent(name string, p float64) error {
	if p < 0 || p > 100 || p != p {
		return fmt.Errorf("fault_injection %s percent must be between 0 and 100", name)
	}

	return nil
}

func (m *FaultInjection) Middleware() (func(http.Handler) http.Handler, error) {
	if m.Delay == nil && m.Abort == nil {
		return nil, errors.New("fault_injection needs delay or abort")
	}

	if m.Delay != nil {
		if m.Delay.Duration <= 0 {
			return nil, errors.New("fault_injection delay duration must be greater than 0")
		}

		if m.Delay.Jitter < 0 {
			return nil, errors.New("fault_injection delay jitter must not be negative")
		}

		if err := checkPercent("delay", m.Delay.Percent); err != nil {
			return nil, err
		}
	}

	abortStatus := http.StatusServiceUnavailable
	if m.Abort != nil {
		if err := checkPercent("abort", m.Abort.Percent); err != nil {
			return nil, err
		}

		if m.Abort.StatusCode != 0 {
			if m.Abort.StatusCode < 200 || m.Abort.StatusCode > 599 {
				return nil, fmt.Errorf("fault_injection abort status_code %d is invalid", m.Abort.StatusCode)
			}

			abortStatus = m.Abort.StatusCode
		}
	}

	header := strings.TrimSpace(m.Header)

	// percent returns the share of the request, -1 when faults are skipped.
	percent := func(r *http.Request, configured float64) float64 {
		if header == "" {
			return configured
		}

		v := strings.TrimSpace(r.Header.Get(header))
		if v == "" || strings.EqualFold(v, "false") {
			return -1
		}

		if p, err := strconv.ParseFloat(v, 64); err == nil && p >= 0 && p <= 100 {
			return p
		}

		return configured
	}

	hit := func(p float64) bool {
		return p > 0 && rand.Float64()*100 < p
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(m.Paths) > 0 {
				matched := false
				for _, p := range m.Paths {
					if strings.HasPrefix(r.URL.Path, p) {
						matched = true

						break
					}
				}

				if !matched {
					next.ServeHTTP(w, r)

					return
				}
			}

			if m.Delay != nil && hit(percent(r, m.Delay.Percent)) {
				d := m.Delay.Duration
				if m.Delay.Jitter > 0 {
					d += rand.N(m.Delay.Jitter)
				}

				t := time.NewTimer(d)
				select {
				case <-t.C:
				case <-r.Context().Done():
					t.Stop()

					return
				}
			}

			if m.Abort != nil && hit(percent(r, m.Abort.Percent)) {
				w.Header().Set("X-Fault-Injected", "abort")
				httputil.HandleError(w, httputil.NewError("fault injected", nil, abortStatus))

				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}
