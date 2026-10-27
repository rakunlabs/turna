package concurrencylimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// blocking returns a handler that holds requests until release is closed.
func blocking(started chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
}

func middleware(t *testing.T, m *ConcurrencyLimit, h http.Handler) http.Handler {
	t.Helper()

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	return mw(h)
}

func request(h http.Handler, header string) int {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("X-Tenant", header)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec.Code
}

func TestLimit(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	h := middleware(t, &ConcurrencyLimit{Max: 2}, blocking(started, release))

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if code := request(h, ""); code != http.StatusNoContent {
				t.Errorf("status = %d", code)
			}
		})
	}

	<-started
	<-started

	if code := request(h, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", code)
	}

	close(release)
	wg.Wait()

	if code := request(h, ""); code != http.StatusNoContent {
		t.Fatalf("status after release = %d", code)
	}
}

func TestQueue(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	h := middleware(t, &ConcurrencyLimit{Max: 1, Queue: 1, QueueTimeout: time.Second}, blocking(started, release))

	var wg sync.WaitGroup
	wg.Go(func() { request(h, "") })
	<-started

	queued := make(chan int, 1)
	go func() { queued <- request(h, "") }()

	// wait until the second request is in the queue
	time.Sleep(20 * time.Millisecond)

	if code := request(h, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("full queue status = %d, want 503", code)
	}

	close(release)
	wg.Wait()

	if code := <-queued; code != http.StatusNoContent {
		t.Fatalf("queued status = %d", code)
	}
}

func TestQueueTimeout(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	defer close(release)

	h := middleware(t, &ConcurrencyLimit{Max: 1, Queue: 5, QueueTimeout: 20 * time.Millisecond, StatusCode: 429}, blocking(started, release))

	go request(h, "")
	<-started

	if code := request(h, ""); code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", code)
	}
}

func TestQueueCanceled(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	h := middleware(t, &ConcurrencyLimit{Max: 1, Queue: 1, QueueTimeout: time.Minute}, blocking(started, release))

	done := make(chan struct{})
	go func() {
		request(h, "")
		close(done)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
		close(waiting)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	<-waiting

	close(release)
	<-done

	// the canceled waiter did not keep a slot
	if code := request(h, ""); code != http.StatusNoContent {
		t.Fatalf("status = %d", code)
	}
	<-started
}

func TestKey(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	h := middleware(t, &ConcurrencyLimit{Max: 1, Key: []string{"header:X-Tenant"}}, blocking(started, release))

	var wg sync.WaitGroup
	wg.Go(func() { request(h, "a") })
	<-started

	if code := request(h, "a"); code != http.StatusServiceUnavailable {
		t.Fatalf("same key status = %d, want 503", code)
	}

	wg.Go(func() {
		if code := request(h, "b"); code != http.StatusNoContent {
			t.Errorf("other key status = %d", code)
		}
	})
	<-started

	close(release)
	wg.Wait()
}

func TestInvalidConfig(t *testing.T) {
	for _, m := range []*ConcurrencyLimit{
		{},
		{Max: 1, Queue: -1},
		{Max: 1, QueueTimeout: -time.Second},
		{Max: 1, Key: []string{"nope"}},
		{Max: 1, TrustedProxies: []string{"bad"}},
	} {
		if _, err := m.Middleware(); err == nil {
			t.Fatalf("%+v: expected error", m)
		}
	}
}
