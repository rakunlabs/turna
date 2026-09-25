package ipallowlist

import (
	"context"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type IPAllowList struct {
	SourceRange []string `cfg:"source_range"`
}

func (m *IPAllowList) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	checker, err := ipcheck.NewChecker(m.SourceRange)
	if err != nil {
		return nil, err
	}

	return tcpmw.Filter(func(conn net.Conn) error {
		return checker.IsAuthorized(conn.RemoteAddr().String())
	}), nil
}
