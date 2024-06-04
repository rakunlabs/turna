package grpcui

import (
	"sync"
	"time"
)

var DefaultTimer = 5 * time.Minute

func NewDebouncer(after time.Duration) func(f func()) {
	if after == 0 {
		after = DefaultTimer
	}

	d := &debouncer{after: after}

	return func(f func()) {
		d.add(f)
	}
}

type debouncer struct {
	mu    sync.Mutex
	after time.Duration
	timer *time.Timer
}

func (d *debouncer) add(f func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.after, f)
}
