package tcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/worldline-go/turna/pkg/server/registry"
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
		middlewares := make([]func(lconn *net.TCPConn) error, 0, len(router.Middlewares))

		for _, middlewareName := range router.Middlewares {
			middleware, err := registry.GlobalReg.GetTcpMiddleware(middlewareName)
			if err != nil {
				return fmt.Errorf("middleware '%s' not found", middlewareName)
			}

			middlewares = append(middlewares, middleware...)
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

			wg.Add(1)
			go func(entrypoint string) {
				defer wg.Done()

				for {
					select {
					case <-ctx.Done():
						return
					default:
					}

					conn, err := listener.AcceptTCP()
					if err != nil {
						if !(errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
							log.Warn().Msgf("failed to accept connection '%s'", err)
						}

						continue
					}

					wg.Add(1)
					go func(conn *net.TCPConn) {
						defer wg.Done()

						<-ctx.Done()

						conn.Close()
					}(conn)

					wg.Add(1)
					go func() {
						defer wg.Done()
						defer conn.Close()

						// do something with conn
						for _, middleware := range middlewares {
							if err := middleware(conn); err != nil {
								log.Warn().Err(err).Msg("middleware failed")

								return
							}
						}
					}()
				}
			}(entrypoint)
		}
	}

	return nil
}
