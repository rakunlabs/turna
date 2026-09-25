package ipdenylist

import (
	"context"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// IPDenyList rejects connections from the given IPs or CIDRs.
type IPDenyList struct {
	SourceRange []string `cfg:"source_range"`
}

func (m *IPDenyList) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	checker, err := ipcheck.NewChecker(m.SourceRange)
	if err != nil {
		return nil, err
	}

	return tcpmw.Filter(func(conn net.Conn) error {
		return ipcheck.Deny(checker, conn.RemoteAddr().String(), tcpmw.ErrReject)
	}), nil
}
