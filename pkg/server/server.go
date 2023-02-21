package server

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/worldline-go/turna/pkg/server/http"
	"github.com/worldline-go/turna/pkg/server/registry"
)

type Server struct {
	LoadValue   string                `cfg:"load_value"`
	EntryPoints map[string]EntryPoint `cfg:"entrypoints"`
	HTTP        http.HTTP             `cfg:"http"`
}

type EntryPoint struct {
	Address string `cfg:"address"`
}

func (e *EntryPoint) Serve(ctx context.Context, name string) error {
	listener, err := net.Listen("tcp", e.Address)
	if err != nil {
		return fmt.Errorf("address cannot listen %s: %w", e.Address, err)
	}

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

	return nil
}
