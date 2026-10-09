package registry

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

var GlobalReg = Registry{
	httpMiddleware: NewNamed[[]func(http.Handler) http.Handler](GroupHTTP),
	tcpMiddleware:  NewNamed[[]tcpmw.Middleware](GroupTCP),
	udpMiddleware:  NewNamed[[]func(conn net.PacketConn, addr net.Addr, data []byte) error](GroupUDP),
	shutdownFuncs:  make(map[string]func()),
	httpInitFuncs:  make(map[string]func() error),
	cleanups:       make(map[*cleanup]struct{}),
}

// Registry holds named middlewares and lifecycle hooks of the server.
//
// Middlewares are stored in Named registries: while a configuration is being
// applied (Begin) new registrations are staged and only become visible to
// requests on Commit.
type Registry struct {
	httpMiddleware *Named[[]func(http.Handler) http.Handler]
	tcpMiddleware  *Named[[]tcpmw.Middleware]
	udpMiddleware  *Named[[]func(conn net.PacketConn, addr net.Addr, data []byte) error]

	shutdownFuncs map[string]func()
	httpInitFuncs map[string]func() error
	cleanups      map[*cleanup]struct{}
	mutex         sync.RWMutex
}

func (r *Registry) RunHTTPInitFuncs() error {
	// Snapshot the init funcs under the read lock. The funcs themselves may
	// call back into the registry (e.g. AddShutdownFunc), which takes the
	// write lock, so they must not run while the lock is held.
	r.mutex.RLock()
	type initFunc struct {
		name string
		f    func() error
	}
	funcs := make([]initFunc, 0, len(r.httpInitFuncs))
	for name, f := range r.httpInitFuncs {
		funcs = append(funcs, initFunc{name: name, f: f})
	}
	r.mutex.RUnlock()

	for _, fn := range funcs {
		if err := fn.f(); err != nil {
			return fmt.Errorf("http init func %s error: %w", fn.name, err)
		}
	}

	return nil
}

// ClearHTTPInitFuncs removes init funcs that already ran so a later reload
// only initializes the middlewares it builds.
func (r *Registry) ClearHTTPInitFuncs() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	clear(r.httpInitFuncs)
}

func (r *Registry) AddInitFunc(name string, f func() error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.httpInitFuncs[name] = f
}

func (r *Registry) AddShutdownFunc(name string, f func()) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.shutdownFuncs[name] = f
}

func (r *Registry) ClearShutdownFunc(name string) {
	r.mutex.Lock()
	f, ok := r.shutdownFuncs[name]
	delete(r.shutdownFuncs, name)
	r.mutex.Unlock()

	if ok {
		f()
	}
}

func (r *Registry) DeleteShutdownFunc(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.shutdownFuncs, name)
}

func (r *Registry) AddTcpMiddleware(name string, m []tcpmw.Middleware) {
	r.tcpMiddleware.Set(name, m)
}

// GetTcpMiddleware returns the middleware visible to the caller: staged
// middlewares while a configuration is being built, otherwise the active one.
func (r *Registry) GetTcpMiddleware(name string) ([]tcpmw.Middleware, error) {
	m, ok := r.tcpMiddleware.Resolve(name)
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

// ActiveTcpMiddleware returns the middleware of the active configuration,
// use it when resolving middlewares while serving connections.
func (r *Registry) ActiveTcpMiddleware(name string) ([]tcpmw.Middleware, error) {
	m, ok := r.tcpMiddleware.Get(name)
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

func (r *Registry) AddUDPMiddleware(name string, m []func(conn net.PacketConn, addr net.Addr, data []byte) error) {
	r.udpMiddleware.Set(name, m)
}

func (r *Registry) GetUDPMiddleware(name string) ([]func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	m, ok := r.udpMiddleware.Resolve(name)
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

func (r *Registry) AddHttpMiddleware(name string, m []func(http.Handler) http.Handler) {
	r.httpMiddleware.Set(name, m)
}

func (r *Registry) DeleteHttpMiddleware(name string) {
	r.httpMiddleware.Delete(name)
}

func (r *Registry) GetHttpMiddleware(name string) ([]func(http.Handler) http.Handler, error) {
	m, ok := r.httpMiddleware.Resolve(name)
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

// ActiveHttpMiddleware returns the middleware of the active configuration,
// use it when resolving middlewares while serving requests.
func (r *Registry) ActiveHttpMiddleware(name string) ([]func(http.Handler) http.Handler, error) {
	m, ok := r.httpMiddleware.Get(name)
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

type cleanup struct {
	name string
	fn   func() error
	once sync.Once
}

func (c *cleanup) run() {
	c.once.Do(func() {
		if err := c.fn(); err != nil {
			slogError(fmt.Sprintf("cleanup [%s] error", c.name), err)
		}
	})
}

// OnStop runs fn when ctx is done, which happens when the middleware owning
// ctx is replaced or removed by a reload. Pending cleanups also run on
// Shutdown, after the servers are stopped.
func (r *Registry) OnStop(ctx context.Context, name string, fn func() error) {
	c := &cleanup{name: name, fn: fn}

	r.mutex.Lock()
	r.cleanups[c] = struct{}{}
	r.mutex.Unlock()

	context.AfterFunc(ctx, func() {
		c.run()

		r.mutex.Lock()
		delete(r.cleanups, c)
		r.mutex.Unlock()
	})
}

// Shutdown runs the shutdown funcs (stopping servers) and then the pending
// cleanups of middlewares.
func (r *Registry) Shutdown() {
	r.mutex.Lock()
	funcs := make([]func(), 0, len(r.shutdownFuncs))
	for _, f := range r.shutdownFuncs {
		funcs = append(funcs, f)
	}
	clear(r.shutdownFuncs)
	r.mutex.Unlock()

	for _, f := range funcs {
		f()
	}

	r.mutex.Lock()
	cleanups := make([]*cleanup, 0, len(r.cleanups))
	for c := range r.cleanups {
		cleanups = append(cleanups, c)
	}
	clear(r.cleanups)
	r.mutex.Unlock()

	for _, c := range cleanups {
		c.run()
	}
}
