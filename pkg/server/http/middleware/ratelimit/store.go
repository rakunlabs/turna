package ratelimit

import (
	"context"
	"sync"
	"time"
)

// result of a single hit.
type result struct {
	allowed bool
	// remaining requests in the window after this hit.
	remaining int
	// reset is the time until the current window ends.
	reset time.Duration
}

type store interface {
	allow(ctx context.Context, key string) (result, error)
}

// slidingWindow approximates a sliding window with the counts of the
// current and the previous fixed window, weighting the previous one by the
// part of it still inside the sliding window.
type slidingWindow struct {
	limit  int
	window time.Duration
}

// windowAt returns the index of the fixed window and the time elapsed in it.
func (s slidingWindow) windowAt(now time.Time) (int64, time.Duration) {
	n := now.UnixNano()
	w := int64(s.window)

	return n / w, time.Duration(n % w)
}

func (s slidingWindow) weight(elapsed time.Duration) float64 {
	return float64(s.window-elapsed) / float64(s.window)
}

func (s slidingWindow) result(allowed bool, rate float64, elapsed time.Duration) result {
	remaining := int(float64(s.limit) - rate)
	if remaining < 0 {
		remaining = 0
	}

	return result{allowed: allowed, remaining: remaining, reset: s.window - elapsed}
}

type memoryStore struct {
	slidingWindow

	mu      sync.Mutex
	entries map[string]*memoryEntry
	lastGC  int64
}

type memoryEntry struct {
	index int64
	curr  int
	prev  int
}

func newMemoryStore(limit int, window time.Duration) *memoryStore {
	return &memoryStore{
		slidingWindow: slidingWindow{limit: limit, window: window},
		entries:       make(map[string]*memoryEntry),
	}
}

func (s *memoryStore) allow(_ context.Context, key string) (result, error) {
	return s.allowAt(time.Now(), key), nil
}

func (s *memoryStore) allowAt(now time.Time, key string) result {
	index, elapsed := s.windowAt(now)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.gc(index)

	e, ok := s.entries[key]
	if !ok {
		e = &memoryEntry{index: index}
		s.entries[key] = e
	}

	switch index - e.index {
	case 0:
	case 1:
		e.prev, e.curr = e.curr, 0
	default:
		e.prev, e.curr = 0, 0
	}
	e.index = index

	rate := float64(e.prev)*s.weight(elapsed) + float64(e.curr)
	if rate+1 > float64(s.limit) {
		return s.result(false, rate, elapsed)
	}

	e.curr++

	return s.result(true, rate+1, elapsed)
}

// gc drops keys idle for two windows, at most once per window.
func (s *memoryStore) gc(index int64) {
	if index == s.lastGC {
		return
	}
	s.lastGC = index

	for k, e := range s.entries {
		if index-e.index > 1 {
			delete(s.entries, k)
		}
	}
}
