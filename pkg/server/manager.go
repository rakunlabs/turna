package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/listener"
	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/udp"
)

// GlobalManager is the manager started by Server.Run, nil before.
var GlobalManager *Manager

// Manager runs a server configuration and applies new configurations at
// runtime: entrypoints are opened and closed, routers and middlewares are
// replaced. Apply is atomic; on error the previous configuration keeps
// running.
type Manager struct {
	ctx context.Context
	wg  *sync.WaitGroup

	http *http.Runtime
	tcp  *tcp.Runtime
	udp  *udp.Runtime

	mu          sync.Mutex
	entrypoints map[string]*listener.Entrypoint
	current     *Server

	layerMu sync.Mutex
	layers  map[string]*Server
}

func NewManager(ctx context.Context, wg *sync.WaitGroup) *Manager {
	return &Manager{
		ctx:         ctx,
		wg:          wg,
		http:        http.NewRuntime(ctx, wg),
		tcp:         tcp.NewRuntime(ctx, wg),
		udp:         udp.NewRuntime(ctx, wg),
		entrypoints: make(map[string]*listener.Entrypoint),
		layers:      make(map[string]*Server),
	}
}

// Current returns the active configuration.
func (m *Manager) Current() *Server {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.current
}

// SetLayer replaces the configuration layer name and applies the merge of
// all layers. Layers let independent sources (static config, kubernetes)
// own parts of the configuration; names must not collide between layers.
// A nil s removes the layer. On error the layer keeps its previous value.
func (m *Manager) SetLayer(name string, s *Server) error {
	m.layerMu.Lock()
	defer m.layerMu.Unlock()

	prev, hadPrev := m.layers[name]

	if s == nil {
		delete(m.layers, name)
	} else {
		m.layers[name] = s
	}

	merged, err := mergeLayers(m.layers)
	if err == nil {
		err = m.Apply(merged)
	}

	if err != nil {
		if hadPrev {
			m.layers[name] = prev
		} else {
			delete(m.layers, name)
		}

		return err
	}

	return nil
}

// Layer returns the configuration of a layer, nil if not set.
func (m *Manager) Layer(name string) *Server {
	m.layerMu.Lock()
	defer m.layerMu.Unlock()

	return m.layers[name]
}

// CheckLayer reports whether replacing the layer name with s would apply,
// without changing the running configuration. Entrypoints are not bound, so
// bind errors of new entrypoints are not detected.
func (m *Manager) CheckLayer(name string, s *Server) error {
	m.layerMu.Lock()
	defer m.layerMu.Unlock()

	layers := maps.Clone(m.layers)
	if s == nil {
		delete(layers, name)
	} else {
		layers[name] = s
	}

	merged, err := mergeLayers(layers)
	if err != nil {
		return err
	}

	return m.check(merged)
}

func (m *Manager) check(s *Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	streams := make([]string, 0, len(s.EntryPoints))
	packets := make([]string, 0)

	for name, cfg := range s.EntryPoints {
		if isPacket(cfg.Network) {
			packets = append(packets, name)
		} else {
			streams = append(streams, name)
		}
	}

	registry.Begin()
	defer registry.Rollback()

	pTCP, err := m.tcp.Prepare(&s.TCP, streams)
	if err != nil {
		return fmt.Errorf("tcp: %w", err)
	}
	defer pTCP.Discard()

	pHTTP, err := m.http.Prepare(&s.HTTP, without(streams, pTCP.Entrypoints()))
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer pHTTP.Discard()

	pUDP, err := m.udp.Prepare(&s.UDP, packets)
	if err != nil {
		return fmt.Errorf("udp: %w", err)
	}
	defer pUDP.Discard()

	return nil
}

// Apply activates s.
func (m *Manager) Apply(s *Server) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// open new sockets first, a bind error aborts without side effects
	opened := make(map[string]*listener.Entrypoint)
	defer func() {
		if err != nil {
			for _, e := range opened {
				_ = e.Close()
			}
		}
	}()

	streams := make([]string, 0, len(s.EntryPoints))
	packets := make([]string, 0)

	for name, cfg := range s.EntryPoints {
		if old, ok := m.entrypoints[name]; !ok || !old.Same(cfg.Address, cfg.Network) {
			e, err := listener.Bind(name, cfg.Address, cfg.Network)
			if err != nil {
				return fmt.Errorf("entrypoint %s: %w", name, err)
			}

			opened[name] = e
		}

		if isPacket(cfg.Network) {
			packets = append(packets, name)
		} else {
			streams = append(streams, name)
		}
	}

	registry.Begin()
	committed := false
	defer func() {
		if !committed {
			registry.Rollback()
		}
	}()

	pTCP, err := m.tcp.Prepare(&s.TCP, streams)
	if err != nil {
		return fmt.Errorf("tcp: %w", err)
	}

	// entrypoints owned by tcp routers are not served by http
	httpEntrypoints := without(streams, pTCP.Entrypoints())

	pHTTP, err := m.http.Prepare(&s.HTTP, httpEntrypoints)
	if err != nil {
		pTCP.Discard()

		return fmt.Errorf("http: %w", err)
	}

	pUDP, err := m.udp.Prepare(&s.UDP, packets)
	if err != nil {
		pTCP.Discard()
		pHTTP.Discard()

		return fmt.Errorf("udp: %w", err)
	}

	// commit
	registry.Commit()
	committed = true

	// stop consumers of entrypoints that are replaced or removed
	for name, old := range m.entrypoints {
		_, replaced := opened[name]
		if _, keep := s.EntryPoints[name]; replaced || !keep {
			m.release(name)
			_ = old.Close()
			delete(m.entrypoints, name)

			slog.Info(fmt.Sprintf("entrypoint %s is closed", name))
		}
	}

	for name, e := range opened {
		m.entrypoints[name] = e
	}

	// release entrypoints that move between tcp and http before activation
	for _, name := range pTCP.Entrypoints() {
		m.http.Release(name)
	}
	for _, name := range httpEntrypoints {
		m.tcp.Release(name)
	}

	pTCP.Activate(m.listen)
	pHTTP.Activate(m.listen)
	pUDP.Activate(m.packet)

	m.current = s

	return nil
}

// Shutdown stops all servers and closes the entrypoints.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.http.Shutdown()

	for name, e := range m.entrypoints {
		m.release(name)

		if err := e.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			slog.Error(fmt.Sprintf("entrypoint [%s] shutdown error", name), "err", err.Error())
		}

		delete(m.entrypoints, name)
	}
}

func (m *Manager) release(name string) {
	m.http.Release(name)
	m.tcp.Release(name)
	m.udp.Release(name)
}

func (m *Manager) listen(name string) (net.Listener, error) {
	e, ok := m.entrypoints[name]
	if !ok {
		return nil, fmt.Errorf("entrypoint %s not found", name)
	}

	return e.Listen()
}

func (m *Manager) packet(name string) (net.PacketConn, error) {
	e, ok := m.entrypoints[name]
	if !ok || e.PacketConn() == nil {
		return nil, fmt.Errorf("udp entrypoint %s not found", name)
	}

	return e.PacketConn(), nil
}

// Addr returns the bound address of an entrypoint.
func (m *Manager) Addr(name string) (net.Addr, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entrypoints[name]
	if !ok {
		return nil, false
	}

	return e.Addr(), true
}

func without(list, remove []string) []string {
	skip := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		skip[r] = struct{}{}
	}

	out := make([]string, 0, len(list))
	for _, v := range list {
		if _, ok := skip[v]; !ok {
			out = append(out, v)
		}
	}

	return out
}
