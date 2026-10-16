package args

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/provider/docker"
	"github.com/rakunlabs/turna/pkg/provider/kubernetes"
	"github.com/rakunlabs/turna/pkg/server"
)

type provider interface {
	Run(ctx context.Context) error
}

// runProviders starts the dynamic configuration providers. They need a
// running server manager, which exists even without static entrypoints.
func runProviders(ctx context.Context, wg *sync.WaitGroup) error {
	providers := config.Application.Providers

	if !providers.Kubernetes.Enabled && !providers.Docker.Enabled {
		return nil
	}

	if server.GlobalManager == nil {
		return fmt.Errorf("providers: server is not running")
	}

	if providers.Kubernetes.Enabled {
		p, err := kubernetes.New(providers.Kubernetes, server.GlobalManager)
		if err != nil {
			return err
		}

		startProvider(ctx, wg, "kubernetes", p)
	}

	if providers.Docker.Enabled {
		p, err := docker.New(providers.Docker, server.GlobalManager)
		if err != nil {
			return err
		}

		startProvider(ctx, wg, "docker", p)
	}

	return nil
}

func startProvider(ctx context.Context, wg *sync.WaitGroup, name string, p provider) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := p.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error(name+" provider stopped", "err", err.Error())
			into.CtxCancel()
		}
	}()
}
