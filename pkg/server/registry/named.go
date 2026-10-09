package registry

import (
	"sync"
	"sync/atomic"
)

// Groups name the namespaces of the named registries. Middlewares of a group
// may register themselves in more than one registry of that group (e.g. a
// session middleware is both an http middleware and a session instance).
const (
	GroupHTTP     = "http"
	GroupTCP      = "tcp"
	GroupUDP      = "udp"
	GroupListener = "listener"
)

type participant interface {
	group() string
	remove(name string)
	commit()
	rollback()
}

var (
	txMu         sync.Mutex
	txActive     atomic.Bool
	participants []participant
	participMu   sync.Mutex
)

// Begin starts a registry transaction. Until Commit or Rollback, Set and
// Delete on every Named registry are staged: readers using Get keep seeing the
// committed values while the configuration under construction resolves staged
// values with Resolve. Only one transaction runs at a time.
func Begin() {
	txMu.Lock()
	txActive.Store(true)
}

// Commit publishes all staged changes and ends the transaction.
func Commit() {
	defer txMu.Unlock()

	forEachParticipant(func(p participant) { p.commit() })
	txActive.Store(false)
}

// Rollback drops all staged changes and ends the transaction.
func Rollback() {
	defer txMu.Unlock()

	forEachParticipant(func(p participant) { p.rollback() })
	txActive.Store(false)
}

// DeleteGroup removes name from every registry of the group. It is used when
// a middleware is rebuilt or removed so stale instances registered under its
// name disappear even if the new middleware has a different type.
func DeleteGroup(group, name string) {
	forEachParticipant(func(p participant) {
		if p.group() == group {
			p.remove(name)
		}
	})
}

func forEachParticipant(fn func(participant)) {
	participMu.Lock()
	list := make([]participant, len(participants))
	copy(list, participants)
	participMu.Unlock()

	for _, p := range list {
		fn(p)
	}
}

// Named is a concurrency safe registry of values by name with support for
// staged updates through Begin/Commit/Rollback.
type Named[T any] struct {
	groupName string

	mu        sync.RWMutex
	committed map[string]T
	// pending holds staged changes, a nil value marks a deletion.
	pending map[string]*T
}

// NewNamed creates a registry that takes part in registry transactions.
func NewNamed[T any](group string) *Named[T] {
	n := &Named[T]{
		groupName: group,
		committed: make(map[string]T),
		pending:   make(map[string]*T),
	}

	participMu.Lock()
	participants = append(participants, n)
	participMu.Unlock()

	return n
}

func (n *Named[T]) group() string {
	return n.groupName
}

// Set stores value, staged when a transaction is active.
func (n *Named[T]) Set(name string, value T) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if txActive.Load() {
		n.pending[name] = &value

		return
	}

	n.committed[name] = value
}

// Delete removes name, staged when a transaction is active.
func (n *Named[T]) Delete(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if txActive.Load() {
		n.pending[name] = nil

		return
	}

	delete(n.committed, name)
}

func (n *Named[T]) remove(name string) {
	n.Delete(name)
}

// Get returns the committed value. Use it while serving requests.
func (n *Named[T]) Get(name string) (T, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	v, ok := n.committed[name]

	return v, ok
}

// Resolve returns the staged value when present, otherwise the committed one.
// Use it while building a configuration.
func (n *Named[T]) Resolve(name string) (T, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if v, ok := n.pending[name]; ok {
		if v == nil {
			var zero T

			return zero, false
		}

		return *v, true
	}

	v, ok := n.committed[name]

	return v, ok
}

// Names returns the committed names.
func (n *Named[T]) Names() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	names := make([]string, 0, len(n.committed))
	for name := range n.committed {
		names = append(names, name)
	}

	return names
}

// ResolveNames returns the names visible to Resolve.
func (n *Named[T]) ResolveNames() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	names := make([]string, 0, len(n.committed)+len(n.pending))
	for name := range n.committed {
		if v, ok := n.pending[name]; ok && v == nil {
			continue
		}

		names = append(names, name)
	}

	for name, v := range n.pending {
		if _, ok := n.committed[name]; !ok && v != nil {
			names = append(names, name)
		}
	}

	return names
}

func (n *Named[T]) commit() {
	n.mu.Lock()
	defer n.mu.Unlock()

	for name, v := range n.pending {
		if v == nil {
			delete(n.committed, name)

			continue
		}

		n.committed[name] = *v
	}

	clear(n.pending)
}

func (n *Named[T]) rollback() {
	n.mu.Lock()
	defer n.mu.Unlock()

	clear(n.pending)
}
