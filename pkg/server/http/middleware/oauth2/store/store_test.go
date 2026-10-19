package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rakunlabs/cache"
)

func TestTakeCodeIsAtomicInMemory(t *testing.T) {
	t.Parallel()

	store, err := (&Store{Active: "memory"}).Init(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close() //nolint:errcheck

	if err := store.Code.Set(t.Context(), "code", "value"); err != nil {
		t.Fatal(err)
	}

	const attempts = 32
	start := make(chan struct{})
	errs := make(chan error, attempts)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			value, ok, err := store.TakeCode(t.Context(), "code")
			if err != nil {
				errs <- err
				return
			}
			if ok {
				if value != "value" {
					errs <- fmt.Errorf("unexpected consumed value %q", value)
					return
				}
				successes.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful consumes = %d, want 1", got)
	}
}

func TestTakeRequiresAtomicStore(t *testing.T) {
	t.Parallel()

	store := &StoreCache{Code: getDeleteOnly{}, State: getDeleteOnly{}}
	if _, _, err := store.TakeCode(t.Context(), "code"); !errors.Is(err, cache.ErrTakeNotSupported) {
		t.Fatalf("TakeCode error = %v, want ErrTakeNotSupported", err)
	}
	if _, _, err := store.TakeState(t.Context(), "state"); !errors.Is(err, cache.ErrTakeNotSupported) {
		t.Fatalf("TakeState error = %v, want ErrTakeNotSupported", err)
	}
}

type getDeleteOnly struct{}

func (getDeleteOnly) Get(context.Context, string) (string, bool, error) { return "v", true, nil }
func (getDeleteOnly) Set(context.Context, string, string) error         { return nil }
func (getDeleteOnly) Delete(context.Context, string) error              { return nil }
