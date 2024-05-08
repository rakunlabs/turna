package session

import "sync"

type Registry struct {
	Store map[string]*Session

	m sync.RWMutex
}

var GlobalRegistry = &Registry{
	Store: make(map[string]*Session),
}

func (r *Registry) Set(name string, store *Session) {
	r.m.Lock()
	defer r.m.Unlock()

	r.Store[name] = store
}

func (r *Registry) Get(name string) *Session {
	r.m.RLock()
	defer r.m.RUnlock()

	return r.Store[name]
}
