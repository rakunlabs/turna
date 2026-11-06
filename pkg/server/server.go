package server

import (
	"context"
	"strings"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/udp"
)

type Server struct {
	LoadValue   string                `cfg:"load_value"`
	EntryPoints map[string]EntryPoint `cfg:"entrypoints"`
	HTTP        http.HTTP             `cfg:"http"`
	TCP         tcp.TCP               `cfg:"tcp"`
	UDP         udp.UDP               `cfg:"udp"`
}

type EntryPoint struct {
	Address string `cfg:"address"`
	Network string `cfg:"network"`
	// HTTP3 serves the TLS routers of the entrypoint over QUIC on the same
	// UDP port too.
	HTTP3 *HTTP3 `cfg:"http3"`
}

type HTTP3 struct {
	Enabled bool `cfg:"enabled"`
	// AdvertisePort is the port announced in the Alt-Svc header, default
	// is the listening port; set it when a NAT or container maps the port.
	AdvertisePort int `cfg:"advertise_port"`
}

func (e EntryPoint) http3() bool {
	return e.HTTP3 != nil && e.HTTP3.Enabled
}

func isPacket(network string) bool {
	return strings.HasPrefix(network, "udp")
}

// Run starts the server and sets GlobalManager to apply later changes.
func (s *Server) Run(ctx context.Context, wg *sync.WaitGroup) error {
	m := NewManager(ctx, wg)
	if err := m.SetLayer(LayerStatic, s); err != nil {
		m.Shutdown()

		return err
	}

	registry.GlobalReg.AddShutdownFunc("server", m.Shutdown)

	GlobalManager = m

	return nil
}
