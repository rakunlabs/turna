// Package keylimit provides token bucket rate limiters keyed by a string such
// as a client IP, with automatic cleanup of idle keys.
package keylimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type Limiter struct {
	rate  rate.Limit
	burst int

	mu      sync.Mutex
	entries map[string]*entry
}

// New creates a limiter allowing r events per second with burst for each key.
// Idle keys are removed until ctx is done.
func New(ctx context.Context, r float64, burst int) *Limiter {
	if burst <= 0 {
		burst = max(1, int(r))
	}

	l := &Limiter{
		rate:    rate.Limit(r),
		burst:   burst,
		entries: make(map[string]*entry),
	}

	go l.cleanup(ctx)

	return l
}

// Allow reports whether an event for key may happen now.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	e, ok := l.entries[key]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = now
	l.mu.Unlock()

	return e.limiter.AllowN(now, 1)
}

// idleTTL is the time after which a key has a full bucket again.
func (l *Limiter) idleTTL() time.Duration {
	ttl := time.Minute
	if l.rate > 0 {
		if full := time.Duration(float64(l.burst) / float64(l.rate) * float64(time.Second)); full > ttl {
			ttl = full
		}
	}

	return ttl
}

func (l *Limiter) cleanup(ctx context.Context) {
	ttl := l.idleTTL()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for k, e := range l.entries {
				if now.Sub(e.lastSeen) > ttl {
					delete(l.entries, k)
				}
			}
			l.mu.Unlock()
		}
	}
}
