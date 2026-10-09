package tcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpchain"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type TCP struct {
	Routers     map[string]Router        `cfg:"routers"`
	Middlewares map[string]TCPMiddleware `cfg:"middlewares"`
}

type Router struct {
	EntryPoints []string `cfg:"entrypoints"`
	Middlewares []string `cfg:"middlewares"`
}

// BuildChain returns the handler running the named middlewares in order.
func BuildChain(names []string) (tcpmw.Handler, error) {
	return tcpchain.Build(names)
}

// ListenerFunc returns a fresh listener for an entrypoint.
type ListenerFunc func(entrypoint string) (net.Listener, error)

// Runtime serves TCP routers and allows replacing them without closing the
// entrypoint sockets. Established connections keep their handler chain.
type Runtime struct {
	ctx context.Context
	wg  *sync.WaitGroup

	components *registry.Components

	mu      sync.Mutex
	servers map[string]*server
}

type server struct {
	listener net.Listener
	handler  *handlerSwitch
	cancel   context.CancelFunc
}

type handlerSwitch struct {
	mu sync.RWMutex
	h  tcpmw.Handler
}

func (s *handlerSwitch) get() tcpmw.Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.h
}

func (s *handlerSwitch) set(h tcpmw.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.h = h
}

func NewRuntime(ctx context.Context, wg *sync.WaitGroup) *Runtime {
	return &Runtime{
		ctx:        ctx,
		wg:         wg,
		components: registry.NewComponents(registry.GroupTCP),
		servers:    make(map[string]*server),
	}
}

// Prepared is a TCP configuration ready to be activated.
type Prepared struct {
	rt       *Runtime
	stage    *registry.ComponentStage
	handlers map[string]tcpmw.Handler
}

// Prepare builds h; it must run inside a registry transaction.
func (rt *Runtime) Prepare(h *TCP, entrypoints []string) (*Prepared, error) {
	available := make(map[string]struct{}, len(entrypoints))
	for _, e := range entrypoints {
		available[e] = struct{}{}
	}

	specs := make(map[string]registry.Spec, len(h.Middlewares))
	for name, m := range h.Middlewares {
		specs[name] = registry.Spec{
			Fingerprint: fingerprint(m),
			Refs:        m.refs(),
			Build: func(ctx context.Context) error {
				return m.Set(ctx, name)
			},
		}
	}

	stage, err := rt.components.Stage(rt.ctx, specs)
	if err != nil {
		return nil, err
	}

	p := &Prepared{rt: rt, stage: stage, handlers: make(map[string]tcpmw.Handler)}

	for name, router := range h.Routers {
		handler, err := BuildChain(router.Middlewares)
		if err != nil {
			stage.Rollback()

			return nil, fmt.Errorf("tcp router %s: %w", name, err)
		}

		for _, entrypoint := range router.EntryPoints {
			if _, ok := available[entrypoint]; !ok {
				stage.Rollback()

				return nil, fmt.Errorf("tcp router %s: entrypoint %s does not exist", name, entrypoint)
			}

			if _, ok := p.handlers[entrypoint]; ok {
				stage.Rollback()

				return nil, fmt.Errorf("tcp router %s: entrypoint %s is used by another tcp router", name, entrypoint)
			}

			p.handlers[entrypoint] = handler
		}
	}

	return p, nil
}

// Entrypoints returns the entrypoints used by the prepared configuration.
func (p *Prepared) Entrypoints() []string {
	out := make([]string, 0, len(p.handlers))
	for e := range p.handlers {
		out = append(out, e)
	}

	return out
}

func (p *Prepared) Discard() {
	p.stage.Rollback()
}

// Activate switches routers to the prepared configuration.
func (p *Prepared) Activate(listen ListenerFunc) {
	rt := p.rt
	p.stage.Commit()

	rt.mu.Lock()
	defer rt.mu.Unlock()

	for entrypoint, s := range rt.servers {
		if _, ok := p.handlers[entrypoint]; !ok {
			rt.stop(entrypoint, s)
		}
	}

	for entrypoint, handler := range p.handlers {
		if s, ok := rt.servers[entrypoint]; ok {
			s.handler.set(handler)

			continue
		}

		listener, err := listen(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get listener [%s]", entrypoint), "err", err.Error())

			continue
		}

		ctx, cancel := context.WithCancel(rt.ctx)
		s := &server{listener: listener, handler: &handlerSwitch{h: handler}, cancel: cancel}
		rt.servers[entrypoint] = s

		serve(ctx, rt.wg, entrypoint, listener, s.handler.get)
	}
}

// Release stops the server of an entrypoint.
func (rt *Runtime) Release(entrypoint string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if s, ok := rt.servers[entrypoint]; ok {
		rt.stop(entrypoint, s)
	}
}

func (rt *Runtime) stop(entrypoint string, s *server) {
	delete(rt.servers, entrypoint)

	_ = s.listener.Close()
	s.cancel()
}

func fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}

func serve(ctx context.Context, wg *sync.WaitGroup, entrypoint string, listener net.Listener, handler func() tcpmw.Handler) {
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer stop()

		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}

				if !errors.Is(err, io.EOF) {
					slog.Warn("failed to accept connection", "entrypoint", entrypoint, "err", err.Error())
				}

				// avoid a busy loop on persistent accept errors (e.g. too many open files)
				time.Sleep(50 * time.Millisecond)

				continue
			}

			h := handler()

			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()

				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()

				if err := h(conn); err != nil {
					if errors.Is(err, tcpmw.ErrReject) {
						slog.Debug("tcp connection rejected", "entrypoint", entrypoint, "remote", conn.RemoteAddr().String(), "err", err.Error())

						return
					}

					slog.Warn("tcp connection failed", "entrypoint", entrypoint, "remote", conn.RemoteAddr().String(), "err", err.Error())
				}
			}()
		}
	}()
}
