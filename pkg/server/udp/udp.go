package udp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

// MaxPacketSize is the read buffer size for a single datagram.
//
// 65535 is the maximum theoretical UDP payload size.
var MaxPacketSize = 65535

// MaxInFlight bounds the number of datagrams handled concurrently per
// entrypoint so a flood of packets cannot spawn unbounded goroutines.
var MaxInFlight = 1024

// Handler processes a single datagram. It may write a response back to the
// peer through conn.WriteTo(resp, addr). Returning an error stops the chain
// and drops the packet.
type Handler = udpmw.Handler

type UDP struct {
	Routers     map[string]Router        `cfg:"routers"`
	Middlewares map[string]UDPMiddleware `cfg:"middlewares"`
}

type Router struct {
	EntryPoints []string `cfg:"entrypoints"`
	Middlewares []string `cfg:"middlewares"`
}

type Middleware struct {
	Name string
	Conn Handler
}

// Runtime serves UDP routers and allows replacing them without closing the
// entrypoint sockets.
type Runtime struct {
	ctx context.Context
	wg  *sync.WaitGroup

	components *registry.Components

	mu      sync.Mutex
	servers map[string]*server
}

type server struct {
	chain  *chainSwitch
	cancel context.CancelFunc
	done   chan struct{}
}

type chainSwitch struct {
	mu sync.RWMutex
	m  []Middleware
}

func (s *chainSwitch) get() []Middleware {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.m
}

func (s *chainSwitch) set(m []Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.m = m
}

// PacketFunc returns the socket of an entrypoint.
type PacketFunc func(entrypoint string) (net.PacketConn, error)

func NewRuntime(ctx context.Context, wg *sync.WaitGroup) *Runtime {
	return &Runtime{
		ctx:        ctx,
		wg:         wg,
		components: registry.NewComponents(registry.GroupUDP),
		servers:    make(map[string]*server),
	}
}

// Prepared is a UDP configuration ready to be activated.
type Prepared struct {
	rt     *Runtime
	stage  *registry.ComponentStage
	chains map[string][]Middleware
}

// Prepare builds h; it must run inside a registry transaction.
func (rt *Runtime) Prepare(h *UDP, entrypoints []string) (*Prepared, error) {
	available := make(map[string]struct{}, len(entrypoints))
	for _, e := range entrypoints {
		available[e] = struct{}{}
	}

	specs := make(map[string]registry.Spec, len(h.Middlewares))
	for name, m := range h.Middlewares {
		specs[name] = registry.Spec{
			Fingerprint: fingerprint(m),
			Build: func(ctx context.Context) error {
				return m.Set(ctx, name)
			},
		}
	}

	stage, err := rt.components.Stage(rt.ctx, specs)
	if err != nil {
		return nil, err
	}

	p := &Prepared{rt: rt, stage: stage, chains: make(map[string][]Middleware)}

	for name, router := range h.Routers {
		middlewares := make([]Middleware, 0, len(router.Middlewares))

		for _, middlewareName := range router.Middlewares {
			middlewaresGet, err := registry.GlobalReg.GetUDPMiddleware(middlewareName)
			if err != nil {
				stage.Rollback()

				return nil, fmt.Errorf("udp router %s: middleware '%s' not found", name, middlewareName)
			}

			for _, m := range middlewaresGet {
				middlewares = append(middlewares, Middleware{Name: middlewareName, Conn: m})
			}
		}

		for _, entrypoint := range router.EntryPoints {
			if _, ok := available[entrypoint]; !ok {
				stage.Rollback()

				return nil, fmt.Errorf("udp router %s: entrypoint %s does not exist", name, entrypoint)
			}

			if _, ok := p.chains[entrypoint]; ok {
				stage.Rollback()

				return nil, fmt.Errorf("udp router %s: entrypoint %s is used by another udp router", name, entrypoint)
			}

			p.chains[entrypoint] = middlewares
		}
	}

	return p, nil
}

func (p *Prepared) Discard() {
	p.stage.Rollback()
}

// Activate switches routers to the prepared configuration.
func (p *Prepared) Activate(conn PacketFunc) {
	rt := p.rt
	p.stage.Commit()

	rt.mu.Lock()
	defer rt.mu.Unlock()

	for entrypoint, s := range rt.servers {
		if _, ok := p.chains[entrypoint]; !ok {
			rt.stop(entrypoint, s)
		}
	}

	for entrypoint, chain := range p.chains {
		if s, ok := rt.servers[entrypoint]; ok {
			s.chain.set(chain)

			continue
		}

		pc, err := conn(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get udp listener [%s]", entrypoint), "err", err.Error())

			continue
		}

		ctx, cancel := context.WithCancel(rt.ctx)
		s := &server{chain: &chainSwitch{m: chain}, cancel: cancel, done: make(chan struct{})}
		rt.servers[entrypoint] = s

		rt.wg.Add(1)
		go func() {
			defer rt.wg.Done()
			defer close(s.done)

			serveLoop(ctx, rt.wg, entrypoint, pc, s.chain.get, false)
		}()
	}
}

// Release stops the server of an entrypoint without closing its socket.
func (rt *Runtime) Release(entrypoint string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if s, ok := rt.servers[entrypoint]; ok {
		rt.stop(entrypoint, s)
	}
}

func (rt *Runtime) stop(entrypoint string, s *server) {
	delete(rt.servers, entrypoint)

	s.cancel()
	<-s.done
}

func fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}

func serve(ctx context.Context, wg *sync.WaitGroup, entrypoint string, conn net.PacketConn, middlewares []Middleware) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		serveLoop(ctx, wg, entrypoint, conn, func() []Middleware { return middlewares }, true)
	}()
}

// serveLoop reads datagrams until ctx is done. The socket is closed on exit
// only when closeConn is set, otherwise a read deadline unblocks the loop so
// the socket can be reused by the next configuration.
func serveLoop(ctx context.Context, wg *sync.WaitGroup, entrypoint string, conn net.PacketConn, chain func() []Middleware, closeConn bool) {
	stop := context.AfterFunc(ctx, func() {
		if closeConn {
			_ = conn.Close()

			return
		}

		_ = conn.SetReadDeadline(time.Now())
	})
	defer stop()

	if !closeConn {
		defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	}

	// bound the number of concurrently handled datagrams.
	sem := make(chan struct{}, MaxInFlight)

	buf := make([]byte, MaxPacketSize)

	for {
		if ctx.Err() != nil {
			return
		}

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}

			slog.Warn("failed to read udp packet", "entrypoint", entrypoint, "err", err.Error())

			continue
		}

		// copy the datagram for the handler goroutine.
		data := make([]byte, n)
		copy(data, buf[:n])

		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		middlewares := chain()

		wg.Add(1)
		go func(addr net.Addr, data []byte) {
			defer wg.Done()
			defer func() { <-sem }()

			c := &udpmw.Conn{PacketConn: conn}

			for _, m := range middlewares {
				if err := m.Conn(c, addr, data); err != nil {
					if errors.Is(err, udpmw.ErrReject) {
						slog.Debug("middleware ["+m.Name+"] dropped packet", "remote", addr.String(), "err", err.Error())

						return
					}

					slog.Warn("middleware ["+m.Name+"] failed", "err", err.Error())

					return
				}
			}
		}(addr, data)
	}
}
