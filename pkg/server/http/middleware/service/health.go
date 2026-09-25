package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HealthCheck actively probes every upstream and removes unhealthy ones from
// the balancer until they recover.
type HealthCheck struct {
	// Path to request, default is "/". It may contain a query string.
	Path string `cfg:"path"`
	// Method default is GET.
	Method string `cfg:"method"`
	// Host overrides the Host header of the probe.
	Host    string            `cfg:"host"`
	Headers map[string]string `cfg:"headers"`
	// Interval between probes, default is 10s.
	Interval time.Duration `cfg:"interval"`
	// Timeout of a probe, default is 5s.
	Timeout time.Duration `cfg:"timeout"`
	// Status is the accepted status codes like "200,204,300-399", default is "200-399".
	Status string `cfg:"status"`
	// HealthyThreshold is the consecutive successes to mark an upstream healthy, default is 1.
	HealthyThreshold int `cfg:"healthy_threshold"`
	// UnhealthyThreshold is the consecutive failures to mark an upstream unhealthy, default is 1.
	UnhealthyThreshold int `cfg:"unhealthy_threshold"`
}

// PassiveHealthCheck ejects an upstream for FailTimeout after MaxFails failed
// requests within FailTimeout.
type PassiveHealthCheck struct {
	// MaxFails is the number of failures to eject the upstream, 0 disables it.
	MaxFails int `cfg:"max_fails"`
	// FailTimeout is the failure counting window and the ejection duration, default is 10s.
	FailTimeout time.Duration `cfg:"fail_timeout"`
	// FailStatuses are response codes counted as failures besides connection errors, e.g. [502, 503, 504].
	FailStatuses []int `cfg:"fail_statuses"`
}

func (p *PassiveHealthCheck) failTimeout() time.Duration {
	if p.FailTimeout <= 0 {
		return 10 * time.Second
	}

	return p.FailTimeout
}

func (p *PassiveHealthCheck) isFailStatus(code int) bool {
	return p != nil && slices.Contains(p.FailStatuses, code)
}

type statusRange struct {
	from, to int
}

func parseStatusRanges(v string) ([]statusRange, error) {
	if strings.TrimSpace(v) == "" {
		return []statusRange{{200, 399}}, nil
	}

	var ranges []statusRange

	for part := range strings.SplitSeq(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fromS, toS, isRange := strings.Cut(part, "-")

		from, err := strconv.Atoi(strings.TrimSpace(fromS))
		if err != nil {
			return nil, fmt.Errorf("invalid status %q", part)
		}

		to := from
		if isRange {
			if to, err = strconv.Atoi(strings.TrimSpace(toS)); err != nil {
				return nil, fmt.Errorf("invalid status %q", part)
			}
		}

		if from < 100 || to > 599 || from > to {
			return nil, fmt.Errorf("invalid status range %q", part)
		}

		ranges = append(ranges, statusRange{from, to})
	}

	return ranges, nil
}

type healthChecker struct {
	cfg      HealthCheck
	ref      *url.URL
	statuses []statusRange
	client   *http.Client
}

func newHealthChecker(cfg *HealthCheck, transport http.RoundTripper) (*healthChecker, error) {
	c := *cfg

	if c.Path == "" {
		c.Path = "/"
	}

	if c.Method == "" {
		c.Method = http.MethodGet
	}

	if c.Interval <= 0 {
		c.Interval = 10 * time.Second
	}

	if c.Timeout <= 0 {
		c.Timeout = 5 * time.Second
	}

	if c.HealthyThreshold <= 0 {
		c.HealthyThreshold = 1
	}

	if c.UnhealthyThreshold <= 0 {
		c.UnhealthyThreshold = 1
	}

	ref, err := url.Parse(c.Path)
	if err != nil {
		return nil, fmt.Errorf("health check path %q: %w", c.Path, err)
	}

	statuses, err := parseStatusRanges(c.Status)
	if err != nil {
		return nil, fmt.Errorf("health check status: %w", err)
	}

	return &healthChecker{
		cfg:      c,
		ref:      ref,
		statuses: statuses,
		client: &http.Client{
			Transport: transport,
			Timeout:   c.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// probeURL returns the health check URL of an upstream; websocket schemes are
// probed over http.
func (h *healthChecker) probeURL(u *url.URL) string {
	target := *u
	switch target.Scheme {
	case "ws":
		target.Scheme = "http"
	case "wss":
		target.Scheme = "https"
	}

	return target.ResolveReference(h.ref).String()
}

func (h *healthChecker) probe(ctx context.Context, u *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, h.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, h.cfg.Method, h.probeURL(u), nil)
	if err != nil {
		return err
	}

	for k, v := range h.cfg.Headers {
		req.Header.Set(k, v)
	}

	if h.cfg.Host != "" {
		req.Host = h.cfg.Host
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()

	for _, r := range h.statuses {
		if resp.StatusCode >= r.from && resp.StatusCode <= r.to {
			return nil
		}
	}

	return fmt.Errorf("unexpected status %d", resp.StatusCode)
}

// run probes the upstream until ctx is done.
func (h *healthChecker) run(ctx context.Context, u *url.URL, state *targetState) {
	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()

	var successes, failures int

	for {
		err := h.probe(ctx, u)
		if ctx.Err() != nil {
			return
		}

		if err == nil {
			failures = 0
			successes++

			if !state.healthy.Load() && successes >= h.cfg.HealthyThreshold {
				state.healthy.Store(true)
				slog.Info("upstream is healthy", "target", u.String())
			}
		} else {
			successes = 0
			failures++

			if state.healthy.Load() && failures >= h.cfg.UnhealthyThreshold {
				state.healthy.Store(false)
				slog.Warn("upstream is unhealthy", "target", u.String(), "err", err.Error())
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
