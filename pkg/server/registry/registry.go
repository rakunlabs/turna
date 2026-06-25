package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

var ShutdownTimeout = 5 * time.Second

var GlobalReg = Registry{
	listeners:      make(map[string]net.Listener),
	udpListeners:   make(map[string]net.PacketConn),
	server:         make(map[string]*http.Server),
	httpMiddleware: make(map[string][]func(http.Handler) http.Handler),
	tcpMiddleware:  make(map[string][]func(lconn *net.TCPConn) error),
	udpMiddleware:  make(map[string][]func(conn net.PacketConn, addr net.Addr, data []byte) error),
	shutdownFuncs:  make(map[string]func()),
	httpInitFuncs:  make(map[string]func() error),
}

type Registry struct {
	listeners      map[string]net.Listener
	udpListeners   map[string]net.PacketConn
	server         map[string]*http.Server
	httpMiddleware map[string][]func(http.Handler) http.Handler
	tcpMiddleware  map[string][]func(lconn *net.TCPConn) error
	udpMiddleware  map[string][]func(conn net.PacketConn, addr net.Addr, data []byte) error
	shutdownFuncs  map[string]func()
	httpInitFuncs  map[string]func() error
	mutex          sync.RWMutex
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

func (r *Registry) AddShutdownFunc(name string, f func()) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.shutdownFuncs[name] = f
}

func (r *Registry) ClearShutdownFunc(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if f, ok := r.shutdownFuncs[name]; ok {
		f()
	}

	delete(r.shutdownFuncs, name)
}

func (r *Registry) DeleteShutdownFunc(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.shutdownFuncs, name)
}

func (r *Registry) AddTcpMiddleware(name string, m []func(lconn *net.TCPConn) error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.tcpMiddleware[name] = m
}

func (r *Registry) GetTcpMiddleware(name string) ([]func(lconn *net.TCPConn) error, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	m, ok := r.tcpMiddleware[name]
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

func (r *Registry) AddUDPMiddleware(name string, m []func(conn net.PacketConn, addr net.Addr, data []byte) error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.udpMiddleware[name] = m
}

func (r *Registry) GetUDPMiddleware(name string) ([]func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	m, ok := r.udpMiddleware[name]
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

func (r *Registry) AddHttpMiddleware(name string, m []func(http.Handler) http.Handler) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.httpMiddleware[name] = m
}

func (r *Registry) AddInitFunc(name string, f func() error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.httpInitFuncs[name] = f
}

func (r *Registry) GetHttpMiddleware(name string) ([]func(http.Handler) http.Handler, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	m, ok := r.httpMiddleware[name]
	if !ok {
		return nil, fmt.Errorf("middleware %s not found", name)
	}

	return m, nil
}

func (r *Registry) AddHttpServer(name string, s *http.Server) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.server[name] = s
}

func (r *Registry) DeleteHttpServer(name string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.server, name)
}

func (r *Registry) GetHttpServer(name string) (*http.Server, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	s, ok := r.server[name]
	if !ok {
		return nil, fmt.Errorf("server %s not found", name)
	}

	return s, nil
}

func (r *Registry) ClearHttpServer(name string) {
	s, err := r.GetHttpServer(name)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		slog.Error(fmt.Sprintf("http [%s] shutdown error", name), "err", err.Error())
	}

	r.DeleteHttpServer(name)
}

func (r *Registry) AddListener(name string, l net.Listener) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.listeners[name] = l
}

func (r *Registry) GetListener(name string) (net.Listener, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	l, ok := r.listeners[name]
	if !ok {
		return nil, fmt.Errorf("listener %s not found", name)
	}

	return l, nil
}

func (r *Registry) GetListenerNames() map[string]struct{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make(map[string]struct{}, len(r.listeners))

	for name := range r.listeners {
		names[name] = struct{}{}
	}

	return names
}

func (r *Registry) GetListenerNamesList() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make([]string, 0, len(r.listeners))

	for name := range r.listeners {
		names = append(names, name)
	}

	return names
}

func (r *Registry) AddUDPListener(name string, l net.PacketConn) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.udpListeners[name] = l
}

func (r *Registry) GetUDPListener(name string) (net.PacketConn, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	l, ok := r.udpListeners[name]
	if !ok {
		return nil, fmt.Errorf("udp listener %s not found", name)
	}

	return l, nil
}

func (r *Registry) GetUDPListenerNames() map[string]struct{} {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	names := make(map[string]struct{}, len(r.udpListeners))

	for name := range r.udpListeners {
		names[name] = struct{}{}
	}

	return names
}

func (r *Registry) ClearUDPListener(name string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	l, ok := r.udpListeners[name]
	if !ok {
		return nil
	}

	if err := l.Close(); err != nil {
		return fmt.Errorf("udp listener %s closed with error: %w", name, err)
	}

	delete(r.udpListeners, name)

	return nil
}

func (r *Registry) ClearListener(name string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	l, ok := r.listeners[name]
	if !ok {
		return nil
	}

	if err := l.Close(); err != nil {
		return fmt.Errorf("listener %s closed with error: %w", name, err)
	}

	delete(r.listeners, name)

	return nil
}

func (r *Registry) Shutdown() {
	// Snapshot all names under the read lock before iterating. The Clear*
	// helpers below take the write lock and delete from these maps, so we must
	// not range over them directly to avoid "concurrent map iteration and map
	// write" if a dynamic config reload mutates the registry concurrently.
	shutdownNames := r.snapshotKeys(func() []string {
		names := make([]string, 0, len(r.shutdownFuncs))
		for name := range r.shutdownFuncs {
			names = append(names, name)
		}
		return names
	})
	serverNames := r.snapshotKeys(func() []string {
		names := make([]string, 0, len(r.server))
		for name := range r.server {
			names = append(names, name)
		}
		return names
	})
	listenerNames := r.snapshotKeys(func() []string {
		names := make([]string, 0, len(r.listeners))
		for name := range r.listeners {
			names = append(names, name)
		}
		return names
	})
	udpListenerNames := r.snapshotKeys(func() []string {
		names := make([]string, 0, len(r.udpListeners))
		for name := range r.udpListeners {
			names = append(names, name)
		}
		return names
	})

	for _, name := range shutdownNames {
		r.ClearShutdownFunc(name)
	}

	for _, name := range serverNames {
		r.ClearHttpServer(name)
	}

	for _, name := range listenerNames {
		if err := r.ClearListener(name); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("listener [%s] shutdown error", name), "err", err.Error())
		} else {
			slog.Warn(fmt.Sprintf("listener [%s] shutdown", name))
		}
	}

	for _, name := range udpListenerNames {
		if err := r.ClearUDPListener(name); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("udp listener [%s] shutdown error", name), "err", err.Error())
		} else {
			slog.Warn(fmt.Sprintf("udp listener [%s] shutdown", name))
		}
	}
}

// snapshotKeys runs collect under the read lock and returns its result. It lets
// Shutdown capture a stable list of map keys before mutating the maps via the
// write-locking Clear* helpers.
func (r *Registry) snapshotKeys(collect func() []string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	return collect()
}
