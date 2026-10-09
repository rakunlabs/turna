package registry

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DrainTimeout delays stopping replaced components so in-flight requests that
// still use them can finish.
var DrainTimeout = 10 * time.Second

// Spec describes a named component (middleware) of a configuration.
type Spec struct {
	// Fingerprint identifies the configuration of the component, an
	// unchanged fingerprint keeps the running instance. Empty means the
	// component is rebuilt on every apply.
	Fingerprint string
	// Refs are names of other components this component resolves by name,
	// a rebuilt reference rebuilds this component too.
	Refs []string
	// Build creates the component and registers it. The context is cancelled
	// when the component is replaced or removed.
	Build func(ctx context.Context) error
}

// Components tracks the running components of a group so a new
// configuration only rebuilds what changed.
type Components struct {
	group string

	mu    sync.Mutex
	built map[string]*component
}

type component struct {
	fingerprint string
	cancel      context.CancelFunc
}

func NewComponents(group string) *Components {
	return &Components{
		group: group,
		built: make(map[string]*component),
	}
}

// ComponentStage holds components built for a pending configuration.
type ComponentStage struct {
	c       *Components
	built   map[string]*component
	removed []string
}

// Stage builds the changed components of specs. It must run inside a
// registry transaction (Begin) so registrations stay invisible to requests
// until Commit.
func (c *Components) Stage(ctx context.Context, specs map[string]Spec) (*ComponentStage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stage := &ComponentStage{c: c, built: make(map[string]*component)}

	dirty := make(map[string]struct{})
	for name, spec := range specs {
		old, ok := c.built[name]
		if !ok || spec.Fingerprint == "" || old.fingerprint != spec.Fingerprint {
			dirty[name] = struct{}{}
		}
	}

	for name := range c.built {
		if _, ok := specs[name]; !ok {
			stage.removed = append(stage.removed, name)
			dirty[name] = struct{}{}
		}
	}

	// rebuild dependents of changed components until stable
	for changed := true; changed; {
		changed = false

		for name, spec := range specs {
			if _, ok := dirty[name]; ok {
				continue
			}

			for _, ref := range spec.Refs {
				if _, ok := dirty[ref]; ok {
					dirty[name] = struct{}{}
					changed = true

					break
				}
			}
		}
	}

	for name := range dirty {
		DeleteGroup(c.group, name)
	}

	// build references first so a component can resolve them eagerly
	order := make([]string, 0, len(dirty))
	visited := make(map[string]bool, len(dirty))

	var visit func(name string)
	visit = func(name string) {
		if _, ok := visited[name]; ok {
			return
		}

		visited[name] = true

		for _, ref := range specs[name].Refs {
			if _, ok := dirty[ref]; ok {
				visit(ref)
			}
		}

		order = append(order, name)
	}

	for name := range dirty {
		visit(name)
	}

	for _, name := range order {
		spec, ok := specs[name]
		if !ok {
			continue
		}

		cctx, cancel := context.WithCancel(ctx)
		stage.built[name] = &component{fingerprint: spec.Fingerprint, cancel: cancel}

		if err := spec.Build(cctx); err != nil {
			stage.Rollback()

			return nil, fmt.Errorf("%s %s: %w", c.group, name, err)
		}
	}

	return stage, nil
}

// Commit makes the staged components current and stops the ones they
// replace after DrainTimeout.
func (s *ComponentStage) Commit() {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()

	for name, comp := range s.built {
		if old, ok := s.c.built[name]; ok {
			stopLater(old.cancel)
		}

		s.c.built[name] = comp
	}

	for _, name := range s.removed {
		if old, ok := s.c.built[name]; ok {
			stopLater(old.cancel)
			delete(s.c.built, name)
		}
	}
}

// Rollback stops the staged components.
func (s *ComponentStage) Rollback() {
	for _, comp := range s.built {
		comp.cancel()
	}
}

// Rebuilt reports the names built by this stage.
func (s *ComponentStage) Rebuilt() []string {
	names := make([]string, 0, len(s.built))
	for name := range s.built {
		names = append(names, name)
	}

	return names
}

func stopLater(cancel context.CancelFunc) {
	if DrainTimeout <= 0 {
		cancel()

		return
	}

	time.AfterFunc(DrainTimeout, cancel)
}
