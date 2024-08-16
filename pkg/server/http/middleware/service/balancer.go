package service

import (
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

type Server struct {
	URL string `cfg:"url"`
}

type (
	// ProxyTarget defines the upstream target.
	ProxyTarget struct {
		Name string
		URL  *url.URL
		Meta map[string]interface{}
	}

	// ProxyBalancer defines an interface to implement a load balancing technique.
	ProxyBalancer interface {
		AddTarget(*ProxyTarget) bool
		RemoveTarget(string) bool
		Next(w http.ResponseWriter, r *http.Request) *ProxyTarget
	}

	// TargetProvider defines an interface that gives the opportunity for balancer
	// to return custom errors when selecting target.
	TargetProvider interface {
		NextTarget(w http.ResponseWriter, r *http.Request) (*ProxyTarget, error)
	}
)

// /////////////////////////////////////////////////////////////////////////////
// RoundRobin Balancer
// /////////////////////////////////////////////////////////////////////////////

// NewRoundRobinBalancer returns a round-robin proxy balancer.
func NewRoundRobinBalancer(targets []*ProxyTarget) ProxyBalancer {
	b := roundRobinBalancer{}
	b.targets = targets
	return &b
}

// RoundRobinBalancer implements a round-robin load balancing technique.
type roundRobinBalancer struct {
	CommonBalancer
	// tracking the index on `targets` slice for the next `*ProxyTarget` to be used
	i int
}

// Next returns an upstream target using round-robin technique. In the case
// where a previously failed request is being retried, the round-robin
// balancer will attempt to use the next target relative to the original
// request. If the list of targets held by the balancer is modified while a
// failed request is being retried, it is possible that the balancer will
// return the original failed target.
//
// Note: `nil` is returned in case upstream target list is empty.
func (b *roundRobinBalancer) Next(w http.ResponseWriter, r *http.Request) *ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.targets) == 0 {
		return nil
	} else if len(b.targets) == 1 {
		return b.targets[0]
	}

	var i int
	const lastIdxKey = "_round_robin_last_index"
	// This request is a retry, start from the index of the previous
	// target to ensure we don't attempt to retry the request with
	// the same failed target
	if v := tcontext.Get(r, lastIdxKey); v != nil {
		i = v.(int)
		i++
		if i >= len(b.targets) {
			i = 0
		}
	} else {
		// This is a first time request, use the global index
		if b.i >= len(b.targets) {
			b.i = 0
		}
		i = b.i
		b.i++
	}

	tcontext.Set(r, lastIdxKey, i)

	return b.targets[i]
}

// /////////////////////////////////////////////////////////////////////////////
// Random Balancer
// /////////////////////////////////////////////////////////////////////////////

// NewRandomBalancer returns a random proxy balancer.
func NewRandomBalancer(targets []*ProxyTarget) ProxyBalancer {
	b := randomBalancer{}
	b.targets = targets
	b.random = rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	return &b
}

// RandomBalancer implements a random load balancing technique.
type randomBalancer struct {
	CommonBalancer
	random *rand.Rand
}

// Next randomly returns an upstream target.
//
// Note: `nil` is returned in case upstream target list is empty.
func (b *randomBalancer) Next(_ http.ResponseWriter, _ *http.Request) *ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.targets) == 0 {
		return nil
	} else if len(b.targets) == 1 {
		return b.targets[0]
	}
	return b.targets[b.random.Intn(len(b.targets))]
}

// /////////////////////////////////////////////////////////////////////////////
// Common Balancer
// /////////////////////////////////////////////////////////////////////////////

type CommonBalancer struct {
	targets []*ProxyTarget
	mutex   sync.Mutex
}

// AddTarget adds an upstream target to the list and returns `true`.
//
// However, if a target with the same name already exists then the operation is aborted returning `false`.
func (b *CommonBalancer) AddTarget(target *ProxyTarget) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for _, t := range b.targets {
		if t.Name == target.Name {
			return false
		}
	}
	b.targets = append(b.targets, target)
	return true
}

// RemoveTarget removes an upstream target from the list by name.
//
// Returns `true` on success, `false` if no target with the name is found.
func (b *CommonBalancer) RemoveTarget(name string) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	for i, t := range b.targets {
		if t.Name == name {
			b.targets = append(b.targets[:i], b.targets[i+1:]...)
			return true
		}
	}
	return false
}

// /////////////////////////////////////////////////////////////////////////////
// PrefixBalancer
// /////////////////////////////////////////////////////////////////////////////

type PrefixBalancer struct {
	CommonBalancer

	Prefixes       []PrefixServers `cfg:"prefixes"`
	DefaultServers []Server        `cfg:"default_servers"`

	DefaultBalancer ProxyBalancer
}

type PrefixServers struct {
	Prefix  string   `cfg:"prefix"`
	Servers []Server `cfg:"servers"`

	Balancer ProxyBalancer
}

func (b *PrefixBalancer) IsEnabled() bool {
	return len(b.Prefixes) > 0 || len(b.DefaultServers) > 0
}

func (b *PrefixBalancer) Next(w http.ResponseWriter, r *http.Request) *ProxyTarget {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	path := r.URL.Path

	for _, prefix := range b.Prefixes {
		if strings.HasPrefix(path, prefix.Prefix) {
			return prefix.Balancer.Next(w, r)
		}
	}

	return b.DefaultBalancer.Next(w, r)
}
