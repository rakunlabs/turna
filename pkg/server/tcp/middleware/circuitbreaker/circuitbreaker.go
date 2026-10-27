// Package circuitbreaker rejects connections for a while when the rest of
// the chain keeps failing, e.g. when the upstream cannot be dialed.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/rakunlabs/turna/pkg/server/breaker"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type CircuitBreaker struct {
	breaker.Config `cfg:",squash"`
}

func (m *CircuitBreaker) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	b, err := breaker.New("circuit_breaker", m.Config)
	if err != nil {
		return nil, err
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			gen, ok, wait := b.Allow()
			if !ok {
				return fmt.Errorf("%w: circuit breaker is open, retry in %s", tcpmw.ErrReject, wait.Round(1e9))
			}

			reported := false
			defer func() {
				if !reported {
					b.Report(gen, true)
				}
			}()

			err := next(conn)
			reported = true

			// rejections of later filters say nothing about the upstream
			if errors.Is(err, tcpmw.ErrReject) {
				b.Release(gen)

				return err
			}

			b.Report(gen, err != nil)

			return err
		}
	}, nil
}
