// Package netlb selects upstream addresses for the TCP and UDP load
// balancers with weighted round-robin, random or least-connections strategies
// and tracks their health.
package netlb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

const (
	StrategyRoundRobin = "round_robin"
	StrategyRandom     = "random"
	StrategyLeastConn  = "least_conn"
)

var ErrNoTarget = errors.New("no upstream available")

type Server struct {
	Address string `cfg:"address"`
	Weight  int    `cfg:"weight"`
}

type PassiveHealthCheck struct {
	// MaxFails within FailTimeout ejects the upstream for FailTimeout.
	MaxFails    int           `cfg:"max_fails"`
	FailTimeout time.Duration `cfg:"fail_timeout"`
}

type Target struct {
	Address string
	Weight  int

	current int
	active  atomic.Int64
	healthy atomic.Bool

	mu           sync.Mutex
	fails        int
	firstFail    time.Time
	ejectedUntil time.Time

	checkSuccess int
	checkFail    int
}

// Active returns the number of in-flight uses.
func (t *Target) Active() int64 { return t.active.Load() }

func (t *Target) available(now time.Time) bool {
	if !t.healthy.Load() {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return !now.Before(t.ejectedUntil)
}

type Balancer struct {
	strategy string
	targets  []*Target
	passive  *PassiveHealthCheck

	mu sync.Mutex
}

func New(strategy string, servers []Server, passive *PassiveHealthCheck) (*Balancer, error) {
	switch strategy {
	case "":
		strategy = StrategyRoundRobin
	case StrategyRoundRobin, StrategyRandom, StrategyLeastConn:
	default:
		return nil, fmt.Errorf("unknown strategy %q", strategy)
	}

	if len(servers) == 0 {
		return nil, errors.New("no servers configured")
	}

	b := &Balancer{strategy: strategy}

	for _, s := range servers {
		if s.Address == "" {
			return nil, errors.New("server address is empty")
		}

		if s.Weight < 0 {
			return nil, fmt.Errorf("server %s has negative weight", s.Address)
		}

		w := s.Weight
		if w == 0 {
			w = 1
		}

		t := &Target{Address: s.Address, Weight: w}
		t.healthy.Store(true)
		b.targets = append(b.targets, t)
	}

	if passive != nil && passive.MaxFails > 0 {
		p := *passive
		if p.FailTimeout <= 0 {
			p.FailTimeout = 10 * time.Second
		}

		b.passive = &p
	}

	return b, nil
}

func (b *Balancer) Targets() []*Target { return b.targets }

// Next returns a target not in exclude. Unavailable targets are skipped; when
// all are unavailable any target not excluded is returned (fail open).
func (b *Balancer) Next(exclude []*Target) (*Target, error) {
	now := time.Now()

	candidates := make([]*Target, 0, len(b.targets))
	for _, t := range b.targets {
		if !contains(exclude, t) && t.available(now) {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		for _, t := range b.targets {
			if !contains(exclude, t) {
				candidates = append(candidates, t)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, ErrNoTarget
	}

	switch b.strategy {
	case StrategyRandom:
		return pickRandom(candidates), nil
	case StrategyLeastConn:
		return pickLeastConn(candidates), nil
	default:
		return b.pickRoundRobin(candidates), nil
	}
}

func contains(list []*Target, t *Target) bool {
	for _, v := range list {
		if v == t {
			return true
		}
	}

	return false
}

// pickRoundRobin is the smooth weighted round-robin of nginx.
func (b *Balancer) pickRoundRobin(candidates []*Target) *Target {
	b.mu.Lock()
	defer b.mu.Unlock()

	total := 0

	var best *Target

	for _, t := range candidates {
		t.current += t.Weight
		total += t.Weight

		if best == nil || t.current > best.current {
			best = t
		}
	}

	best.current -= total

	return best
}

func pickRandom(candidates []*Target) *Target {
	total := 0
	for _, t := range candidates {
		total += t.Weight
	}

	n := rand.IntN(total) //nolint:gosec // load balancing
	for _, t := range candidates {
		n -= t.Weight
		if n < 0 {
			return t
		}
	}

	return candidates[len(candidates)-1]
}

func pickLeastConn(candidates []*Target) *Target {
	var best *Target

	var bestScore float64

	for _, t := range candidates {
		score := float64(t.active.Load()+1) / float64(t.Weight)
		if best == nil || score < bestScore {
			best, bestScore = t, score
		}
	}

	return best
}

// Acquire marks a use of t, the returned function ends it.
func (b *Balancer) Acquire(t *Target) func() {
	t.active.Add(1)

	var once sync.Once

	return func() { once.Do(func() { t.active.Add(-1) }) }
}

// ReportFailure counts a failure for passive health checking.
func (b *Balancer) ReportFailure(t *Target) {
	if b.passive == nil {
		return
	}

	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.fails == 0 || now.Sub(t.firstFail) > b.passive.FailTimeout {
		t.fails = 0
		t.firstFail = now
	}

	t.fails++
	if t.fails >= b.passive.MaxFails {
		t.ejectedUntil = now.Add(b.passive.FailTimeout)
		t.fails = 0

		slog.Warn("upstream ejected", "address", t.Address, "for", b.passive.FailTimeout.String())
	}
}

// ReportSuccess resets the failure counter.
func (b *Balancer) ReportSuccess(t *Target) {
	if b.passive == nil {
		return
	}

	t.mu.Lock()
	t.fails = 0
	t.mu.Unlock()
}

// HealthCheck is an active health check run for every target.
type HealthCheck struct {
	Interval           time.Duration `cfg:"interval"`
	Timeout            time.Duration `cfg:"timeout"`
	HealthyThreshold   int           `cfg:"healthy_threshold"`
	UnhealthyThreshold int           `cfg:"unhealthy_threshold"`
}

// StartHealthCheck probes every target until ctx is done.
func (b *Balancer) StartHealthCheck(ctx context.Context, hc HealthCheck, probe func(ctx context.Context, address string) error) {
	if hc.Interval <= 0 {
		hc.Interval = 10 * time.Second
	}

	if hc.Timeout <= 0 {
		hc.Timeout = 5 * time.Second
	}

	if hc.HealthyThreshold <= 0 {
		hc.HealthyThreshold = 1
	}

	if hc.UnhealthyThreshold <= 0 {
		hc.UnhealthyThreshold = 1
	}

	for _, t := range b.targets {
		go func(t *Target) {
			ticker := time.NewTicker(hc.Interval)
			defer ticker.Stop()

			for {
				b.check(ctx, hc, t, probe)

				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(t)
	}
}

func (b *Balancer) check(ctx context.Context, hc HealthCheck, t *Target, probe func(ctx context.Context, address string) error) {
	pctx, cancel := context.WithTimeout(ctx, hc.Timeout)
	err := probe(pctx, t.Address)
	cancel()

	if ctx.Err() != nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if err == nil {
		t.checkFail = 0
		t.checkSuccess++

		if !t.healthy.Load() && t.checkSuccess >= hc.HealthyThreshold {
			t.healthy.Store(true)
			slog.Info("upstream healthy", "address", t.Address)
		}

		return
	}

	t.checkSuccess = 0
	t.checkFail++

	if t.healthy.Load() && t.checkFail >= hc.UnhealthyThreshold {
		t.healthy.Store(false)
		slog.Warn("upstream unhealthy", "address", t.Address, "err", err.Error())
	}
}
