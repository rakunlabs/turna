// Package concurrencylimit bounds the number of requests handled at the same
// time.
package concurrencylimit

import (
	"container/list"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/requestkey"
)

type ConcurrencyLimit struct {
	// Max is the number of requests handled at the same time per key.
	Max int `cfg:"max"`
	// Queue is the number of requests per key waiting for a slot, 0 rejects
	// requests over Max right away.
	Queue int `cfg:"queue"`
	// QueueTimeout is the longest wait for a slot, default 10s.
	QueueTimeout time.Duration `cfg:"queue_timeout"`

	// Key selects what requests are counted by, parts are combined:
	//   ip, realip, path, host, method, header:<name>, query:<name>, cookie:<name>
	// Default is one limit for all requests.
	Key []string `cfg:"key"`
	// TrustedProxies are CIDRs whose forwarding headers are used for the ip key.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// StatusCode of rejected requests, default is 503.
	StatusCode int `cfg:"status_code"`
}

type state struct {
	running int
	// waiters are channels of queued requests in arrival order
	waiters list.List
}

type limiter struct {
	mu    sync.Mutex
	keys  map[string]*state
	max   int
	queue int
}

// acquire returns ok when the request may run now. Otherwise a non nil
// wait channel is closed when a slot is handed to the request; nil means the
// queue is full.
func (l *limiter) acquire(key string) (ok bool, wait chan struct{}, elem *list.Element) {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.keys[key]
	if s == nil {
		s = &state{}
		l.keys[key] = s
	}

	if s.running < l.max {
		s.running++

		return true, nil, nil
	}

	if s.waiters.Len() >= l.queue {
		return false, nil, nil
	}

	wait = make(chan struct{})

	return false, wait, s.waiters.PushBack(wait)
}

// release frees a slot or hands it to the next waiting request.
func (l *limiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := l.keys[key]

	if front := s.waiters.Front(); front != nil {
		s.waiters.Remove(front)
		close(front.Value.(chan struct{}))

		return
	}

	s.running--
	if s.running == 0 {
		delete(l.keys, key)
	}
}

// leave removes a waiting request. It reports false when the slot was handed
// over meanwhile and must be released.
func (l *limiter) leave(key string, elem *list.Element) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	select {
	case <-elem.Value.(chan struct{}):
		return false
	default:
	}

	l.keys[key].waiters.Remove(elem)

	return true
}

func (m *ConcurrencyLimit) Middleware() (func(http.Handler) http.Handler, error) {
	if m.Max <= 0 {
		return nil, errors.New("concurrency_limit max must be greater than 0")
	}

	if m.Queue < 0 {
		return nil, errors.New("concurrency_limit queue must not be negative")
	}

	if m.QueueTimeout < 0 {
		return nil, errors.New("concurrency_limit queue_timeout must not be negative")
	}

	queueTimeout := m.QueueTimeout
	if queueTimeout == 0 {
		queueTimeout = 10 * time.Second
	}

	keyFunc, err := requestkey.New("concurrency_limit", m.Key, m.TrustedProxies)
	if err != nil {
		return nil, err
	}

	status := m.StatusCode
	if status == 0 {
		status = http.StatusServiceUnavailable
	}

	l := &limiter{keys: make(map[string]*state), max: m.Max, queue: m.Queue}

	reject := func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "1")
		httputil.HandleError(w, httputil.NewError("too many concurrent requests", nil, status))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)

			ok, wait, elem := l.acquire(key)
			if !ok {
				if wait == nil {
					reject(w)

					return
				}

				t := time.NewTimer(queueTimeout)

				select {
				case <-wait:
					t.Stop()
				case <-t.C:
					if l.leave(key, elem) {
						reject(w)

						return
					}
				case <-r.Context().Done():
					t.Stop()

					if l.leave(key, elem) {
						return
					}

					l.release(key)

					return
				}
			}

			defer l.release(key)

			next.ServeHTTP(w, r)
		})
	}, nil
}
