package udp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/registry"
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
type Handler = func(conn net.PacketConn, addr net.Addr, data []byte) error

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

func (h *UDP) Set(ctx context.Context, wg *sync.WaitGroup) error {
	for name, middleware := range h.Middlewares {
		if err := middleware.Set(ctx, name); err != nil {
			return err
		}
	}

	for _, router := range h.Routers {
		middlewares := make([]Middleware, 0, len(router.Middlewares))

		for _, middlewareName := range router.Middlewares {
			middlewaresGet, err := registry.GlobalReg.GetUDPMiddleware(middlewareName)
			if err != nil {
				return fmt.Errorf("middleware '%s' not found", middlewareName)
			}

			for _, m := range middlewaresGet {
				middlewares = append(middlewares, Middleware{Name: middlewareName, Conn: m})
			}
		}

		for _, entrypoint := range router.EntryPoints {
			conn, err := registry.GlobalReg.GetUDPListener(entrypoint)
			if err != nil {
				return err
			}

			serve(ctx, wg, entrypoint, conn, middlewares)
		}
	}

	return nil
}

func serve(ctx context.Context, wg *sync.WaitGroup, entrypoint string, conn net.PacketConn, middlewares []Middleware) {
	// close the connection when the context is cancelled so ReadFrom unblocks.
	wg.Add(1)
	go func() {
		defer wg.Done()

		<-ctx.Done()

		conn.Close()
	}()

	// bound the number of concurrently handled datagrams.
	sem := make(chan struct{}, MaxInFlight)

	wg.Add(1)
	go func() {
		defer wg.Done()

		buf := make([]byte, MaxPacketSize)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
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

			wg.Add(1)
			go func(addr net.Addr, data []byte) {
				defer wg.Done()
				defer func() { <-sem }()

				for _, m := range middlewares {
					if err := m.Conn(conn, addr, data); err != nil {
						slog.Warn("middleware ["+m.Name+"] failed", "err", err.Error())

						return
					}
				}
			}(addr, data)
		}
	}()
}
