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

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/rakunlabs/turna/pkg/server/acme"
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
	tls   atomic.Pointer[tlsConfigs]
	// acme answers HTTP-01 challenges on plain entrypoints
	acme atomic.Pointer[acme.Manager]

	components *registry.Components

	mu      sync.Mutex
	servers map[string]*runningServer

	// acmeCurrent is the active ACME manager; Prepare and Activate run under
	// the manager lock.
	acmeCurrent *acme.Manager
}

type runningServer struct {
	server   *http.Server
	listener net.Listener
	// h3 serves the entrypoint over QUIC, nil without http3
	h3 *http3.Server
	// h3ln is the QUIC listener of h3; closed before the transport gets
	// a new listener
	h3ln *quic.EarlyListener
	// quic is the HTTP/3 setting the server started with
	quic QUIC
	// timeouts the server started with
	timeouts Timeouts
	// cancel ends the base context of the requests
	cancel context.CancelFunc

	// open TLS connections with the client certificate settings they were
	// accepted with
	connMu sync.Mutex
	conns  map[net.Conn]string
}

// closeStale closes the connections accepted with other client certificate
// settings than auth: tightened validation must not keep serving clients
// that would be refused now.
func (s *runningServer) closeStale(auth string) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	for c, a := range s.conns {
		if a != auth {
			_ = c.Close()
			delete(s.conns, c)
		}
	}
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
	tls    *tlsConfigs
	stage  *registry.ComponentStage
	plain  map[string]struct{}
	secure map[string]struct{}

	// acme is the manager to activate; newACME reports that it was built by
	// this Prepare and must be started or stopped.
	acme    *acme.Manager
	newACME bool

	quic     map[string]QUIC
	timeouts map[string]Timeouts
}

// QUIC is the HTTP/3 transport of an entrypoint.
type QUIC struct {
	Transport *quic.Transport
	// AdvertisePort overrides the port of the Alt-Svc header.
	AdvertisePort int
	// Allow0RTT accepts 0-RTT early data, which can be replayed.
	Allow0RTT bool
}

// Timeouts of the HTTP server of an entrypoint. Zero read and write
// timeouts are unlimited, a zero idle timeout is IdleTimeout.
type Timeouts struct {
	Read  time.Duration
	Write time.Duration
	Idle  time.Duration
}

// timeoutsFor returns the server timeouts of an entrypoint.
func (p *Prepared) timeoutsFor(entrypoint string) Timeouts {
	t := p.timeouts[entrypoint]
	if t.Idle == 0 {
		t.Idle = IdleTimeout
	}

	return t
}

// SetTimeouts sets the server timeouts of the entrypoints, used by
// Activate; servers with other timeouts are restarted.
func (p *Prepared) SetTimeouts(t map[string]Timeouts) {
	p.timeouts = t
}

// quicFor returns the HTTP/3 setting of an entrypoint; only TLS servers
// serve HTTP/3.
func (p *Prepared) quicFor(entrypoint string, secure bool) QUIC {
	if !secure {
		return QUIC{}
	}

	return p.quic[entrypoint]
}

// SetQUIC sets the HTTP/3 sockets of the entrypoints, used by Activate for
// TLS entrypoints.
func (p *Prepared) SetQUIC(q map[string]QUIC) {
	p.quic = q
}

// Prepare builds h for the given entrypoints. It must run inside a registry
// transaction; nothing is visible to requests until Activate.
func (rt *Runtime) Prepare(h *HTTP, entrypoints []string) (*Prepared, error) {
	plain, secure, err := h.entrypoints(entrypoints)
	if err != nil {
		return nil, err
	}

	p := &Prepared{rt: rt, plain: plain, secure: secure}

	if err := p.prepareACME(h); err != nil {
		return nil, err
	}

	if len(secure) > 0 {
		p.tls, err = h.buildTLSConfigs(secure, p.acme)
		if err != nil {
			p.discardACME()

			return nil, err
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
		p.discardACME()

		return nil, err
	}

	err = registry.GlobalReg.RunHTTPInitFuncs()
	registry.GlobalReg.ClearHTTPInitFuncs()
	if err != nil {
		p.Discard()

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
			p.Discard()

			return nil, fmt.Errorf("router %s cannot set: %w", name, err)
		}
	}

	return p, nil
}

// Discard drops a prepared configuration that will not be activated.
func (p *Prepared) Discard() {
	p.stage.Rollback()
	p.discardACME()
}

// prepareACME reuses the running ACME manager when its configuration is
// unchanged, otherwise builds a new one that is started on Activate.
func (p *Prepared) prepareACME(h *HTTP) error {
	if h.TLS.ACME == nil || !h.TLS.ACME.Enabled {
		return nil
	}

	if cur := p.rt.acmeCurrent; cur != nil && cur.Fingerprint() == h.TLS.ACME.Fingerprint() {
		p.acme = cur

		return nil
	}

	m, err := acme.New(p.rt.ctx, h.TLS.ACME)
	if err != nil {
		return err
	}

	p.acme = m
	p.newACME = true

	return nil
}

func (p *Prepared) discardACME() {
	if p.newACME && p.acme != nil {
		p.acme.Stop()
		p.acme = nil
		p.newACME = false
	}
}

func (rt *Runtime) activateACME(p *Prepared) {
	if p.newACME {
		if err := p.acme.Start(); err != nil {
			slog.Error("acme cannot start", "err", err.Error())
		}
	}

	rt.acme.Store(p.acme)

	if old := rt.acmeCurrent; old != nil && old != p.acme {
		old.Stop()
	}

	rt.acmeCurrent = p.acme
	p.newACME = false
}

// Activate switches to the prepared configuration. Servers are started for
// new entrypoints and stopped for entrypoints without routers. listen is used
// for entrypoints that need a new server.
func (p *Prepared) Activate(listen ListenerFunc) {
	rt := p.rt

	if p.tls != nil {
		rt.tls.Store(p.tls)
	}

	rt.activateACME(p)

	rt.rules.Store(p.rules)
	p.stage.Commit()

	rt.mu.Lock()

	wanted := make(map[string]bool, len(p.plain)+len(p.secure))
	for e := range p.plain {
		wanted[e] = false
	}
	for e := range p.secure {
		wanted[e] = true
	}

	stopped := make(map[string]*runningServer)

	for key, s := range rt.servers {
		entrypoint, secure := splitServerKey(key)
		if want, ok := wanted[entrypoint]; ok && want == secure && s.quic == p.quicFor(entrypoint, secure) && s.timeouts == p.timeoutsFor(entrypoint) {
			continue
		}

		rt.detach(key, s)
		stopped[key] = s
	}

	for entrypoint, secure := range wanted {
		if s, ok := rt.servers[serverKey(entrypoint, secure)]; ok {
			if secure {
				s.closeStale(rt.tls.Load().clientAuthKey(entrypoint))
			}

			continue
		}

		listener, err := listen(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get listener [%s]", entrypoint), "err", err.Error())

			continue
		}

		if _, ok := p.quic[entrypoint]; ok && !secure {
			slog.Warn("http3 needs tls routers, entrypoint is served without http3", "entrypoint", entrypoint)
		}

		var quic *QUIC
		if q := p.quicFor(entrypoint, secure); q.Transport != nil {
			quic = &q
		}

		rt.startServer(entrypoint, secure, listener, quic, p.timeoutsFor(entrypoint))
	}

	rt.mu.Unlock()

	shutdownServers(stopped)
}

// Release stops the servers of the given entrypoints, used before the
// entrypoint socket is closed or handed to another protocol.
func (rt *Runtime) Release(entrypoint string) {
	rt.mu.Lock()

	stopped := make(map[string]*runningServer)

	for _, secure := range []bool{false, true} {
		key := serverKey(entrypoint, secure)
		if s, ok := rt.servers[key]; ok {
			rt.detach(key, s)
			stopped[key] = s
		}
	}

	rt.mu.Unlock()

	shutdownServers(stopped)
}

// Shutdown gracefully stops all servers.
func (rt *Runtime) Shutdown() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	stopped := make(map[string]*runningServer, len(rt.servers))

	for key, s := range rt.servers {
		rt.detach(key, s)
		stopped[key] = s
	}

	shutdownServers(stopped)

	if rt.acmeCurrent != nil {
		rt.acmeCurrent.Stop()
		rt.acmeCurrent = nil
		rt.acme.Store(nil)
	}
}

// handler serves an entrypoint; plain entrypoints answer ACME HTTP-01
// challenges before routing.
func (rt *Runtime) handler(entrypoint string, secure bool) http.Handler {
	next := rt.rules.Serve(entrypoint)
	if secure {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := rt.acme.Load(); m != nil && m.HandleHTTPChallenge(w, r) {
			return
		}

		next.ServeHTTP(w, r)
	})
}

// detach removes a server and stops it accepting at once; the socket stays
// open for the next consumer. shutdownServers drains it.
func (rt *Runtime) detach(key string, s *runningServer) {
	delete(rt.servers, key)

	_ = s.listener.Close()

	// the transport accepts a new listener only after the old one is closed
	if s.h3ln != nil {
		_ = s.h3ln.Close()
	}
}

// shutdownServers gracefully stops detached servers in parallel; connections
// still open after ShutdownTimeout, hijacked ones included, are closed.
func shutdownServers(servers map[string]*runningServer) {
	var wg sync.WaitGroup

	for key, s := range servers {
		wg.Go(func() { shutdownServer(key, s) })
	}

	wg.Wait()
}

func shutdownServer(key string, s *runningServer) {
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if s.h3 != nil {
		if err := s.h3.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			slog.Error(fmt.Sprintf("http3 [%s] shutdown error", key), "err", err.Error())
		}

		_ = s.h3.Close()
	}

	if err := s.server.Shutdown(ctx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			slog.Error(fmt.Sprintf("http [%s] shutdown error", key), "err", err.Error())
		}

		_ = s.server.Close()
	}

	// Shutdown and Close do not track hijacked connections; proxied upgrades
	// (websockets) end with the request context
	s.cancel()
}

func (rt *Runtime) startServer(entrypoint string, secure bool, listener net.Listener, quic *QUIC, timeouts Timeouts) {
	key := serverKey(entrypoint, secure)

	handler := rt.handler(entrypoint, secure)

	baseCtx, cancel := context.WithCancel(context.Background())

	s := &http.Server{
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       timeouts.Read,
		WriteTimeout:      timeouts.Write,
		IdleTimeout:       timeouts.Idle,
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}

	if !secure {
		// gRPC clients without TLS use HTTP/2 prior knowledge (h2c)
		var protocols http.Protocols
		protocols.SetHTTP1(true)
		protocols.SetUnencryptedHTTP2(true)
		s.Protocols = &protocols
	}

	running := &runningServer{server: s, listener: listener, timeouts: timeouts, cancel: cancel, conns: make(map[net.Conn]string)}

	if secure {
		s.TLSConfig = &tls.Config{
			// Resolve the active TLS config on every handshake so certificate
			// changes apply to new connections without restarting the server.
			GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
				configs := rt.tls.Load()

				// remember the client certificate settings the connection
				// is accepted with
				if hello.Conn != nil {
					running.connMu.Lock()
					running.conns[hello.Conn] = configs.clientAuthKey(entrypoint)
					running.connMu.Unlock()
				}

				return configs.forEntrypoint(entrypoint), nil
			},
		}

		s.ConnState = func(c net.Conn, state http.ConnState) {
			if state != http.StateClosed && state != http.StateHijacked {
				return
			}

			if tc, ok := c.(*tls.Conn); ok {
				c = tc.NetConn()
			}

			running.connMu.Lock()
			delete(running.conns, c)
			running.connMu.Unlock()
		}
	}

	if quic != nil {
		running.quic = *quic
		running.h3, running.h3ln = rt.startHTTP3(key, entrypoint, handler, quic)
		if running.h3 != nil {
			s.Handler = altSvc(handler, listener.Addr(), quic.AdvertisePort)
		}
	}

	rt.servers[key] = running

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

// startHTTP3 serves handler over QUIC with the TLS configuration of the
// entrypoint.
func (rt *Runtime) startHTTP3(key, entrypoint string, handler http.Handler, q *QUIC) (*http3.Server, *quic.EarlyListener) {
	h3 := &http3.Server{Handler: handler}

	tlsConf := http3.ConfigureTLSConfig(&tls.Config{
		MinVersion: tls.VersionTLS13,
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return rt.tls.Load().forEntrypoint(entrypoint), nil
		},
	})

	// a listener of the long-lived transport, closing it keeps the socket
	// usable for the next server of the entrypoint
	ln, err := q.Transport.ListenEarly(tlsConf, &quic.Config{Allow0RTT: q.Allow0RTT})
	if err != nil {
		slog.Error(fmt.Sprintf("cannot listen http3 [%s]", key), "err", err.Error())

		return nil, nil
	}

	rt.wg.Add(1)
	go func() {
		defer rt.wg.Done()

		slog.Info(fmt.Sprintf("http3 server [%s] is listening on %s", key, ln.Addr().String()))

		if err := h3.ServeListener(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, quic.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("cannot serve http3 [%s]", key), "err", err.Error())
		}

		slog.Info(fmt.Sprintf("http3 server [%s] is stopped", key))
	}()

	return h3, ln
}

// altSvc announces HTTP/3 on the port of addr or advertisePort.
func altSvc(next http.Handler, addr net.Addr, advertisePort int) http.Handler {
	port := advertisePort
	if port == 0 {
		if a, ok := addr.(*net.TCPAddr); ok {
			port = a.Port
		}
	}

	value := fmt.Sprintf(`h3=":%d"; ma=86400`, port)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor < 3 {
			w.Header().Add("Alt-Svc", value)
		}

		next.ServeHTTP(w, r)
	})
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
	case h.Canary != nil:
		add(h.Canary.Refs()...)
	case h.Errors != nil:
		add(h.Errors.Refs()...)
	case h.InfoMiddleware != nil:
		add(h.InfoMiddleware.SessionMiddleware)
	case h.SessionInfoMiddleware != nil:
		add(h.SessionInfoMiddleware.SessionMiddleware)
	case h.LoginMiddleware != nil:
		add(h.LoginMiddleware.SessionMiddleware, h.LoginMiddleware.AuthMiddleware)
	case h.IamForwardAuthMiddleware != nil:
		add(h.IamForwardAuthMiddleware.AuthMiddleware)
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
