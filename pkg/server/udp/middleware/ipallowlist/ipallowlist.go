package ipallowlist

import (
	"context"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

type IPAllowList struct {
	SourceRange []string `cfg:"source_range"`
}

func (m *IPAllowList) Middleware(_ context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	checker, err := ipcheck.NewChecker(m.SourceRange)
	if err != nil {
		return nil, err
	}

	return func(_ net.PacketConn, addr net.Addr, _ []byte) error {
		return checker.IsAuthorized(addr.String())
	}, nil
}
