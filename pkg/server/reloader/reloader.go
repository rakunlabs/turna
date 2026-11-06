// Package reloader connects reload requests, such as the reload middleware,
// to the application that knows how to reload the configuration.
package reloader

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrNotAvailable is returned when no reload function is set.
var ErrNotAvailable = errors.New("reload is not available")

// Func reloads the configuration; reason is logged.
type Func func(ctx context.Context, reason string) error

var current atomic.Pointer[Func]

// Set registers the reload function, nil removes it.
func Set(fn Func) {
	if fn == nil {
		current.Store(nil)

		return
	}

	current.Store(&fn)
}

// Reload runs the registered reload function.
func Reload(ctx context.Context, reason string) error {
	fn := current.Load()
	if fn == nil {
		return ErrNotAvailable
	}

	return (*fn)(ctx, reason)
}
