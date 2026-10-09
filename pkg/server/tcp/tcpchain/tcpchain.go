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

// Build returns the handler running the named middlewares in order. It
// resolves middlewares of a configuration that is being applied.
func Build(names []string) (tcpmw.Handler, error) {
	return build(names, registry.GlobalReg.GetTcpMiddleware)
}

func build(names []string, get func(string) ([]tcpmw.Middleware, error)) (tcpmw.Handler, error) {
	middlewares := make([]tcpmw.Middleware, 0, len(names))

	for _, name := range names {
		got, err := get(name)
		if err != nil {
			return nil, fmt.Errorf("middleware '%s' not found", name)
		}

		for _, m := range got {
			middlewares = append(middlewares, withName(name, m))
		}
	}

	return tcpmw.Chain(func(net.Conn) error { return nil }, middlewares...), nil
}

// Lazy builds the chain on first use from the active middlewares, so
// middlewares may be registered in any order. A middleware using Lazy is
// rebuilt by a reload when one of the names changes.
func Lazy(names []string) tcpmw.Handler {
	var (
		once    sync.Once
		handler tcpmw.Handler
		err     error
	)

	return func(conn net.Conn) error {
		once.Do(func() { handler, err = build(names, registry.GlobalReg.ActiveTcpMiddleware) })
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
