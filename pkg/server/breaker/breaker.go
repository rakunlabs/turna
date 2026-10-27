// Package breaker implements a circuit breaker shared by HTTP and TCP
// middlewares.
package breaker

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Config of a breaker, zero values use the defaults.
type Config struct {
	// FailureRatio opens the circuit when failed / total requests in Window
	// reaches it, default 0.5.
	FailureRatio float64 `cfg:"failure_ratio"`
	// MinRequests is the number of requests in Window needed before the
	// ratio is checked, default 10.
	MinRequests int `cfg:"min_requests"`
	// Window is the rolling time window of counted requests, default 10s.
	Window time.Duration `cfg:"window"`
	// OpenDuration is how long requests are rejected after the circuit
	// opens, default 30s.
	OpenDuration time.Duration `cfg:"open_duration"`
	// HalfOpenRequests is the number of probe requests let through after
	// OpenDuration; all must succeed to close the circuit, default 1.
	HalfOpenRequests int `cfg:"half_open_requests"`
}

const buckets = 10

type state int

const (
	closed state = iota
	open
	halfOpen
)

type bucket struct {
	start    time.Time
	total    int
	failures int
}

// Breaker is a circuit breaker counting results in a rolling window.
type Breaker struct {
	mu  sync.Mutex
	now func() time.Time

	ratio        float64
	minRequests  int
	window       time.Duration
	openDuration time.Duration
	probes       int

	state    state
	openedAt time.Time
	// generation changes with every state change so results of requests
	// started in an earlier state are ignored
	generation uint64
	buckets    [buckets]bucket
	inFlight   int
	succeeded  int
}

// New returns a breaker of cfg with defaults applied. name prefixes
// configuration errors.
func New(name string, cfg Config) (*Breaker, error) {
	b := &Breaker{
		now:          time.Now,
		ratio:        cfg.FailureRatio,
		minRequests:  cfg.MinRequests,
		window:       cfg.Window,
		openDuration: cfg.OpenDuration,
		probes:       cfg.HalfOpenRequests,
	}

	if b.ratio == 0 {
		b.ratio = 0.5
	}
	if b.ratio < 0 || b.ratio > 1 || math.IsNaN(b.ratio) {
		return nil, fmt.Errorf("%s failure_ratio %v must be between 0 and 1", name, cfg.FailureRatio)
	}

	if b.minRequests == 0 {
		b.minRequests = 10
	}
	if b.minRequests < 0 {
		return nil, fmt.Errorf("%s min_requests must not be negative", name)
	}

	if b.window == 0 {
		b.window = 10 * time.Second
	}
	if b.window < 0 {
		return nil, fmt.Errorf("%s window must not be negative", name)
	}

	if b.openDuration == 0 {
		b.openDuration = 30 * time.Second
	}
	if b.openDuration < 0 {
		return nil, fmt.Errorf("%s open_duration must not be negative", name)
	}

	if b.probes == 0 {
		b.probes = 1
	}
	if b.probes < 0 {
		return nil, fmt.Errorf("%s half_open_requests must not be negative", name)
	}

	return b, nil
}

// Allow reports whether a request may pass and returns the generation to
// report its result with. When rejected, wait is the time until the next
// probe is let through.
func (b *Breaker) Allow() (gen uint64, ok bool, wait time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()

	if b.state == open {
		if wait := b.openDuration - now.Sub(b.openedAt); wait > 0 {
			return 0, false, wait
		}

		b.setState(halfOpen, now)
	}

	if b.state == halfOpen {
		if b.inFlight+b.succeeded >= b.probes {
			return 0, false, time.Second
		}

		b.inFlight++
	}

	return b.generation, true, 0
}

// Report records the result of a request allowed with generation gen.
func (b *Breaker) Report(gen uint64, failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if gen != b.generation {
		return
	}

	now := b.now()

	switch b.state {
	case halfOpen:
		b.inFlight--

		if failed {
			b.setState(open, now)

			return
		}

		b.succeeded++
		if b.succeeded >= b.probes {
			b.setState(closed, now)
		}
	case closed:
		cur := b.bucket(now)
		cur.total++
		if failed {
			cur.failures++
		}

		total, failures := b.count(now)
		if total >= b.minRequests && total > 0 && float64(failures)/float64(total) >= b.ratio {
			b.setState(open, now)
		}
	}
}

// Release gives back a half-open slot of a request without a result, such
// as one canceled by the client.
func (b *Breaker) Release(gen uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if gen == b.generation && b.state == halfOpen {
		b.inFlight--
	}
}

func (b *Breaker) setState(s state, now time.Time) {
	b.state = s
	b.generation++
	b.inFlight = 0
	b.succeeded = 0
	b.buckets = [buckets]bucket{}

	if s == open {
		b.openedAt = now
	}
}

func (b *Breaker) bucketSize() time.Duration {
	return max(b.window/buckets, time.Nanosecond)
}

func (b *Breaker) bucket(now time.Time) *bucket {
	size := b.bucketSize()
	start := now.Truncate(size)
	cur := &b.buckets[(start.UnixNano()/int64(size))%buckets]

	if !cur.start.Equal(start) {
		*cur = bucket{start: start}
	}

	return cur
}

func (b *Breaker) count(now time.Time) (total, failures int) {
	oldest := now.Truncate(b.bucketSize()).Add(-b.window)

	for _, bk := range b.buckets {
		if bk.start.After(oldest) {
			total += bk.total
			failures += bk.failures
		}
	}

	return total, failures
}
