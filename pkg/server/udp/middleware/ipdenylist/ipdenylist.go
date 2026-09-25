package ipdenylist

import (
	"context"
	"net"

	"github.com/rakunlabs/turna/pkg/server/ipcheck"
	"github.com/rakunlabs/turna/pkg/server/udp/udpmw"
)

// IPDenyList drops datagrams from the given IPs or CIDRs.
type IPDenyList struct {
	SourceRange []string `cfg:"source_range"`
}

func (m *IPDenyList) Middleware(_ context.Context, _ string) (udpmw.Handler, error) {
	checker, err := ipcheck.NewChecker(m.SourceRange)
	if err != nil {
		return nil, err
	}

	return func(_ net.PacketConn, addr net.Addr, _ []byte) error {
		return ipcheck.Deny(checker, addr.String(), udpmw.ErrReject)
	}, nil
}
