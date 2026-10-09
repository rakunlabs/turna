package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rakunlabs/turna/pkg/server/registry"
)

// ShutdownTimeout bounds graceful shutdown of a removed HTTP server.
var ShutdownTimeout = 5 * time.Second

// ListenerFunc returns a fresh listener for an entrypoint. Each call replaces
// the previous listener of that entrypoint.
type ListenerFunc func(entrypoint string) (net.Listener, error)

// Runtime owns the running HTTP servers and allows replacing routers,
// middlewares and TLS settings without restarting listeners.
type Runtime struct {
	ctx context.Context
	wg  *sync.WaitGroup

	rules *RuleSwitch
	tls   atomic.Pointer[tls.Config]

	components *registry.Components

	mu      sync.Mutex
	servers map[string]*runningServer
}

type runningServer struct {
	server   *http.Server
	listener net.Listener
}

func NewRuntime(ctx context.Context, wg *sync.WaitGroup) *Runtime {
	return &Runtime{
		ctx:        ctx,
		wg:         wg,
		rules:      NewRuleSwitch(),
		components: registry.NewComponents(registry.GroupHTTP),
		servers:    make(map[string]*runningServer),
	}
}

// Prepared is an HTTP configuration ready to be activated.
type Prepared struct {
	rt     *Runtime
	rules  *RuleRouter
	tls    *tls.Config
	stage  *registry.ComponentStage
	plain  map[string]struct{}
	secure map[string]struct{}
}

// Prepare builds h for the given entrypoints. It must run inside a registry
// transaction; nothing is visible to requests until Activate.
func (rt *Runtime) Prepare(h *HTTP, entrypoints []string) (*Prepared, error) {
	plain, secure, err := h.entrypoints(entrypoints)
	if err != nil {
		return nil, err
	}

	p := &Prepared{rt: rt, plain: plain, secure: secure}

	if len(secure) > 0 {
		p.tls, err = h.buildTLSConfig()
		if err != nil {
			return nil, err
		}

		// GetConfigForClient bypasses the HTTP/2 setup of ServeTLS, so the
		// returned config must advertise ALPN protocols itself.
		if len(p.tls.NextProtos) == 0 {
			p.tls.NextProtos = []string{"h2", "http/1.1"}
		}
	}

	specs := make(map[string]registry.Spec, len(h.Middlewares))
	for name, m := range h.Middlewares {
		specs[name] = registry.Spec{
			Fingerprint: m.fingerprint(),
			Refs:        m.refs(),
			Build: func(ctx context.Context) error {
				return m.Set(ctx, name)
			},
		}
	}

	p.stage, err = rt.components.Stage(rt.ctx, specs)
	if err != nil {
		registry.GlobalReg.ClearHTTPInitFuncs()

		return nil, err
	}

	err = registry.GlobalReg.RunHTTPInitFuncs()
	registry.GlobalReg.ClearHTTPInitFuncs()
	if err != nil {
		p.stage.Rollback()

		return nil, fmt.Errorf("cannot init http middlewares: %w", err)
	}

	p.rules = NewRuleRouter()
	for _, router := range h.Routers {
		for _, entrypoint := range router.entrypointsFrom(entrypoints) {
			p.rules.SetRule(RuleSelection{Host: router.Host, Entrypoint: entrypoint})
		}
	}

	for name, router := range h.Routers {
		if err := router.set(p.rules, entrypoints); err != nil {
			p.stage.Rollback()

			return nil, fmt.Errorf("router %s cannot set: %w", name, err)
		}
	}

	return p, nil
}

// Discard drops a prepared configuration that will not be activated.
func (p *Prepared) Discard() {
	p.stage.Rollback()
}

// Activate switches to the prepared configuration. Servers are started for
// new entrypoints and stopped for entrypoints without routers. listen is used
// for entrypoints that need a new server.
func (p *Prepared) Activate(listen ListenerFunc) {
	rt := p.rt

	if p.tls != nil {
		rt.tls.Store(p.tls)
	}

	rt.rules.Store(p.rules)
	p.stage.Commit()

	rt.mu.Lock()
	defer rt.mu.Unlock()

	wanted := make(map[string]bool, len(p.plain)+len(p.secure))
	for e := range p.plain {
		wanted[e] = false
	}
	for e := range p.secure {
		wanted[e] = true
	}

	for key, s := range rt.servers {
		entrypoint, secure := splitServerKey(key)
		if want, ok := wanted[entrypoint]; ok && want == secure {
			continue
		}

		rt.stopServer(key, s)
	}

	for entrypoint, secure := range wanted {
		if _, ok := rt.servers[serverKey(entrypoint, secure)]; ok {
			continue
		}

		listener, err := listen(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get listener [%s]", entrypoint), "err", err.Error())

			continue
		}

		rt.startServer(entrypoint, secure, listener)
	}
}

// Release stops the servers of the given entrypoints, used before the
// entrypoint socket is closed or handed to another protocol.
func (rt *Runtime) Release(entrypoint string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	for _, secure := range []bool{false, true} {
		key := serverKey(entrypoint, secure)
		if s, ok := rt.servers[key]; ok {
			rt.stopServer(key, s)
		}
	}
}

// Shutdown gracefully stops all servers.
func (rt *Runtime) Shutdown() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	for key, s := range rt.servers {
		rt.stopServer(key, s)
	}
}

func (rt *Runtime) stopServer(key string, s *runningServer) {
	delete(rt.servers, key)

	// stop accepting at once, the socket stays open for the next consumer
	_ = s.listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		slog.Error(fmt.Sprintf("http [%s] shutdown error", key), "err", err.Error())
	}
}

func (rt *Runtime) startServer(entrypoint string, secure bool, listener net.Listener) {
	key := serverKey(entrypoint, secure)

	s := &http.Server{
		ReadHeaderTimeout: ReadHeaderTimeout,
		Handler:           rt.rules.Serve(entrypoint),
	}

	if !secure {
		// gRPC clients without TLS use HTTP/2 prior knowledge (h2c)
		var protocols http.Protocols
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		s.Protocols = &protocols
	}

	if secure {
		s.TLSConfig = &tls.Config{
			// Resolve the active TLS config on every handshake so certificate
			// changes apply to new connections without restarting the server.
			GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
				return rt.tls.Load(), nil
			},
		}
	}

	rt.servers[key] = &runningServer{server: s, listener: listener}

	rt.wg.Add(1)
	go func() {
		defer rt.wg.Done()

		slog.Info(fmt.Sprintf("http server [%s] is listening on %s", key, listener.Addr().String()))

		var err error
		if secure {
			err = s.ServeTLS(listener, "", "")
		} else {
			err = s.Serve(listener)
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("cannot serve listener [%s]", key), "err", err.Error())
		}

		slog.Info(fmt.Sprintf("http server [%s] is stopped", key))
	}()
}

func serverKey(entrypoint string, secure bool) string {
	if secure {
		return entrypoint + "-TLS"
	}

	return entrypoint
}

func splitServerKey(key string) (string, bool) {
	if e, ok := cutSuffix(key, "-TLS"); ok {
		return e, true
	}

	return key, false
}

func cutSuffix(s, suffix string) (string, bool) {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)], true
	}

	return s, false
}

// entrypoints validates router entrypoints and returns the plain and TLS
// entrypoints that need a running server.
func (h *HTTP) entrypoints(available []string) (plain, secure map[string]struct{}, err error) {
	all := make(map[string]struct{}, len(available))
	for _, e := range available {
		all[e] = struct{}{}
	}

	plain = make(map[string]struct{})
	secure = make(map[string]struct{})

	for name, router := range h.Routers {
		for _, entrypoint := range router.EntryPoints {
			if _, ok := all[entrypoint]; !ok {
				return nil, nil, fmt.Errorf("router %s: entrypoint %s does not exist", name, entrypoint)
			}
		}

		for _, entrypoint := range router.entrypointsFrom(available) {
			if router.TLS != nil {
				secure[entrypoint] = struct{}{}
			} else {
				plain[entrypoint] = struct{}{}
			}
		}
	}

	for e := range secure {
		if _, ok := plain[e]; ok {
			return nil, nil, fmt.Errorf("entrypoint %s is used by both tls and non-tls routers", e)
		}
	}

	return plain, secure, nil
}

// fingerprint returns a stable representation of the middleware config used
// to detect changes between reloads.
func (h HTTPMiddleware) fingerprint() string {
	v, err := json.Marshal(h)
	if err != nil {
		return ""
	}

	return string(v)
}

// refs returns names of middlewares this middleware resolves by name. When a
// referenced middleware is rebuilt, this one is rebuilt too.
func (h HTTPMiddleware) refs() []string {
	var refs []string

	add := func(names ...string) {
		for _, n := range names {
			if n != "" {
				refs = append(refs, n)
			}
		}
	}

	switch {
	case h.SplitterMiddleware != nil:
		for i := range h.SplitterMiddleware.Rules {
			add(h.SplitterMiddleware.Rules[i].Middlewares...)
		}
	case h.Gateway != nil:
		add(h.Gateway.Refs()...)
	case h.InfoMiddleware != nil:
		add(h.InfoMiddleware.SessionMiddleware)
	case h.SessionInfoMiddleware != nil:
		add(h.SessionInfoMiddleware.SessionMiddleware)
	case h.LoginMiddleware != nil:
		add(h.LoginMiddleware.SessionMiddleware, h.LoginMiddleware.AuthMiddleware)
	case h.Oauth2 != nil:
		add(h.Oauth2.IamMiddleware)
	case h.IamForwardAuthMiddleware != nil:
		if h.IamForwardAuthMiddleware.IamMiddleware != nil {
			add(*h.IamForwardAuthMiddleware.IamMiddleware)
		}
	case h.IamCheckMiddleware != nil:
		add(h.IamCheckMiddleware.AuthMiddleware)
	case h.OAuth2Resource != nil:
		add(h.OAuth2Resource.AuthMiddleware)
	case h.SessionMiddleware != nil:
		add(h.SessionMiddleware.AuthSkipPaths...)
		for _, p := range h.SessionMiddleware.Provider {
			add(p.AuthMiddleware)
		}
		if h.SessionMiddleware.ProviderSource != nil {
			add(h.SessionMiddleware.ProviderSource.AuthMiddleware)
		}
	}

	return refs
}
