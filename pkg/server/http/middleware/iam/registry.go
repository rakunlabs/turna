package iam

import "github.com/rakunlabs/turna/pkg/server/registry"

// Registry holds iam middleware instances by middleware name. Updates made
// while a configuration is applied are staged until it is committed.
type Registry struct {
	named *registry.Named[*Iam]
}

var GlobalRegistry = &Registry{
	named: registry.NewNamed[*Iam](registry.GroupHTTP),
}

func (r *Registry) Set(name string, store *Iam) {
	r.named.Set(name, store)
}

// Get returns the active instance by name, use it while serving requests.
func (r *Registry) Get(name string) *Iam {
	v, _ := r.named.Get(name)

	return v
}

// Resolve returns the instance by name including ones staged by a
// configuration that is being applied, use it while building middlewares.
func (r *Registry) Resolve(name string) *Iam {
	v, _ := r.named.Resolve(name)

	return v
}
