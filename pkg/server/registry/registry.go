package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

var (
	ShutdownTimeout = 5 * time.Second
)

var GlobalReg = Registry{
	listeners:      make(map[string]net.Listener),
	server:         make(map[string]*http.Server),
	httpMiddleware: make(map[string][]echo.MiddlewareFunc),
	shutdownFuncs:  make(map[string]func()),
}

type Registry struct {
	listeners      map[string]net.Listener
	server         map[string]*http.Server
	httpMiddleware map[string][]echo.MiddlewareFunc
	shutdownFuncs  map[string]func()
	mutex          sync.RWMutex
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

func (r *Registry) AddHttpMiddleware(name string, m []echo.MiddlewareFunc) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.httpMiddleware[name] = m
}

func (r *Registry) GetHttpMiddleware(name string) ([]echo.MiddlewareFunc, error) {
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
		log.Error().Err(err).Msgf("http [%s] shutdown error", name)
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
	for name := range r.shutdownFuncs {
		r.ClearShutdownFunc(name)
	}

	for name := range r.server {
		r.ClearHttpServer(name)
	}

	for name := range r.listeners {
		if err := r.ClearListener(name); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Error().Err(err).Msgf("listener [%s] shutdown error", name)
		} else {
			log.Warn().Msgf("listener [%s] shutdown", name)
		}
	}
}
