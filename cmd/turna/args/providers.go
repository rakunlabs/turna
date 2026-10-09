package args

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/provider/kubernetes"
	"github.com/rakunlabs/turna/pkg/server"
)

// runProviders starts the dynamic configuration providers. They need a
// running server manager, which exists even without static entrypoints.
func runProviders(ctx context.Context, wg *sync.WaitGroup) error {
	k8s := config.Application.Providers.Kubernetes
	if !k8s.Enabled {
		return nil
	}

	if server.GlobalManager == nil {
		return fmt.Errorf("kubernetes provider: server is not running")
	}

	p, err := kubernetes.New(k8s, server.GlobalManager)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := p.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("kubernetes provider stopped", "err", err.Error())
			into.CtxCancel()
		}
	}()

	return nil
}
