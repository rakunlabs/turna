package iam

import "sync"

type Registry struct {
	Store map[string]*Iam

	m sync.RWMutex
}

var GlobalRegistry = &Registry{
	Store: make(map[string]*Iam),
}

func (r *Registry) Set(name string, store *Iam) {
	r.m.Lock()
	defer r.m.Unlock()

	r.Store[name] = store
}

func (r *Registry) Get(name string) *Iam {
	r.m.RLock()
	defer r.m.RUnlock()

	return r.Store[name]
}
