// Package block drops a share of UDP datagrams.
package block

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"

	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

// Block drops a percentage of datagrams and passes the others on.
type Block struct {
	// Percent of datagrams to drop, 0-100. Default is 100.
	Percent float64 `cfg:"percent"`
}

func (m *Block) Middleware(_ context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	percent := m.Percent
	if percent == 0 {
		percent = 100
	}

	if percent < 0 || percent > 100 {
		return nil, fmt.Errorf("percent must be between 0 and 100: %v", percent)
	}

	return func(net.PacketConn, net.Addr, []byte) error {
		if percent >= 100 || rand.Float64()*100 < percent {
			return fmt.Errorf("%w: blocked", udpmw.ErrReject)
		}

		return nil
	}, nil
}
