package tcp

import (
	"context"
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

func (h *TCP) Set(ctx context.Context, wg *sync.WaitGroup) error {
	for name, middleware := range h.Middlewares {
		if err := middleware.Set(ctx, name); err != nil {
			return err
		}
	}

	for _, router := range h.Routers {
		handler, err := BuildChain(router.Middlewares)
		if err != nil {
			return err
		}

		for _, entrypoint := range router.EntryPoints {
			listenerRaw, err := registry.GlobalReg.GetListener(entrypoint)
			if err != nil {
				return err
			}

			listener, ok := listenerRaw.(*net.TCPListener)
			if !ok {
				return fmt.Errorf("listener '%s' is not a TCP listener", entrypoint)
			}

			serve(ctx, wg, entrypoint, listener, handler)
		}
	}

	return nil
}

// BuildChain returns the handler running the named middlewares in order.
func BuildChain(names []string) (tcpmw.Handler, error) {
	return tcpchain.Build(names)
}

func serve(ctx context.Context, wg *sync.WaitGroup, entrypoint string, listener *net.TCPListener, handler tcpmw.Handler) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			conn, err := listener.AcceptTCP()
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

			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()

				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()

				if err := handler(conn); err != nil {
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
