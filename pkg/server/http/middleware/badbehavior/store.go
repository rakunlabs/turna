package badbehavior

import (
	"context"
	"sync"
	"time"
)

type store interface {
	// banned returns the remaining ban time of ip, 0 when it is not banned.
	banned(ctx context.Context, ip string) (time.Duration, error)
	// hit counts a bad response of ip and reports whether it got banned now.
	hit(ctx context.Context, ip string) (bool, error)
}

// memoryStore counts bad responses in a fixed window starting at the first
// hit of a client.
type memoryStore struct {
	threshold int
	window    time.Duration
	banTime   time.Duration

	mu      sync.Mutex
	counter map[string]*counter
	bans    map[string]time.Time
}

type counter struct {
	count int
	reset time.Time
}

func newMemoryStore(ctx context.Context, threshold int, window, banTime time.Duration) *memoryStore {
	s := &memoryStore{
		threshold: threshold,
		window:    window,
		banTime:   banTime,
		counter:   make(map[string]*counter),
		bans:      make(map[string]time.Time),
	}

	go s.cleanup(ctx, min(window, banTime, time.Minute))

	return s
}

func (s *memoryStore) banned(_ context.Context, ip string) (time.Duration, error) {
	return s.bannedAt(time.Now(), ip), nil
}

func (s *memoryStore) bannedAt(now time.Time, ip string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	until, ok := s.bans[ip]
	if !ok {
		return 0
	}

	if !now.Before(until) {
		delete(s.bans, ip)

		return 0
	}

	return until.Sub(now)
}

func (s *memoryStore) hit(_ context.Context, ip string) (bool, error) {
	return s.hitAt(time.Now(), ip), nil
}

func (s *memoryStore) hitAt(now time.Time, ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// requests already running when the ban started
	if until, ok := s.bans[ip]; ok && now.Before(until) {
		return false
	}

	c, ok := s.counter[ip]
	if !ok || !now.Before(c.reset) {
		c = &counter{reset: now.Add(s.window)}
		s.counter[ip] = c
	}

	c.count++
	if c.count < s.threshold {
		return false
	}

	delete(s.counter, ip)
	s.bans[ip] = now.Add(s.banTime)

	return true
}

func (s *memoryStore) cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.gc(now)
		}
	}
}

func (s *memoryStore) gc(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for ip, c := range s.counter {
		if !now.Before(c.reset) {
			delete(s.counter, ip)
		}
	}

	for ip, until := range s.bans {
		if !now.Before(until) {
			delete(s.bans, ip)
		}
	}
}
