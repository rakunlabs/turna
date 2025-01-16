package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/worldline-go/turna/pkg/server/http"
	"github.com/worldline-go/turna/pkg/server/registry"
	"github.com/worldline-go/turna/pkg/server/tcp"
)

type Server struct {
	LoadValue   string                `cfg:"load_value"`
	EntryPoints map[string]EntryPoint `cfg:"entrypoints"`
	HTTP        http.HTTP             `cfg:"http"`
	TCP         tcp.TCP               `cfg:"tcp"`
}

type EntryPoint struct {
	Address string `cfg:"address"`
	Network string `cfg:"network"`
}

func (e *EntryPoint) Serve(ctx context.Context, name string) error {
	network := e.Network
	if network == "" {
		network = "tcp"
	}

	listener, err := net.Listen(network, e.Address)
	if err != nil {
		return fmt.Errorf("address cannot listen %s: %w", e.Address, err)
	}

	slog.Info(fmt.Sprintf("entrypoint %s is listening on %s with %s", name, e.Address, network))

	registry.GlobalReg.AddListener(name, listener)

	return nil
}

func (s *Server) Run(ctx context.Context, wg *sync.WaitGroup) error {
	if len(s.EntryPoints) == 0 {
		return nil
	}

	for name, entrypoint := range s.EntryPoints {
		if err := entrypoint.Serve(ctx, name); err != nil {
			return fmt.Errorf("entrypoint %s cannot serve: %w", name, err)
		}
	}

	if err := s.HTTP.Set(ctx, wg); err != nil {
		return fmt.Errorf("http cannot set: %w", err)
	}

	if err := s.TCP.Set(ctx, wg); err != nil {
		return fmt.Errorf("tcp cannot set: %w", err)
	}

	return nil
}
