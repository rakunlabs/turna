// Package block rejects a share of TCP connections.
package block

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// Block closes a percentage of connections and passes the others on.
type Block struct {
	// Percent of connections to close, 0-100. Default is 100.
	Percent float64 `cfg:"percent"`
}

func (m *Block) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	percent := m.Percent
	if percent == 0 {
		percent = 100
	}

	if percent < 0 || percent > 100 {
		return nil, fmt.Errorf("percent must be between 0 and 100: %v", percent)
	}

	return tcpmw.Filter(func(net.Conn) error {
		if percent >= 100 || rand.Float64()*100 < percent {
			return fmt.Errorf("%w: blocked", tcpmw.ErrReject)
		}

		return nil
	}), nil
}
