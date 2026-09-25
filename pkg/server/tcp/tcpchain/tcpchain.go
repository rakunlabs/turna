// Package tcpchain builds TCP middleware chains from registered middleware
// names. It is separate from package tcp so middlewares can build sub chains.
package tcpchain

import (
	"fmt"
	"net"
	"sync"

	"github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// Build returns the handler running the named middlewares in order.
func Build(names []string) (tcpmw.Handler, error) {
	middlewares := make([]tcpmw.Middleware, 0, len(names))

	for _, name := range names {
		got, err := registry.GlobalReg.GetTcpMiddleware(name)
		if err != nil {
			return nil, fmt.Errorf("middleware '%s' not found", name)
		}

		for _, m := range got {
			middlewares = append(middlewares, withName(name, m))
		}
	}

	return tcpmw.Chain(func(net.Conn) error { return nil }, middlewares...), nil
}

// Lazy builds the chain on first use, middlewares may be registered in any
// order during startup.
func Lazy(names []string) tcpmw.Handler {
	var (
		once    sync.Once
		handler tcpmw.Handler
		err     error
	)

	return func(conn net.Conn) error {
		once.Do(func() { handler, err = Build(names) })
		if err != nil {
			return err
		}

		return handler(conn)
	}
}

// withName prefixes errors of a middleware with its name, errors coming from
// the rest of the chain are passed unchanged.
func withName(name string, m tcpmw.Middleware) tcpmw.Middleware {
	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			var nextErr error

			err := m(func(conn net.Conn) error {
				nextErr = next(conn)

				return nextErr
			})(conn)
			if err == nil || err == nextErr { //nolint:errorlint // identity check
				return err
			}

			return fmt.Errorf("middleware [%s]: %w", name, err)
		}
	}
}
