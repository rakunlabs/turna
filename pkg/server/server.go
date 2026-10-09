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
