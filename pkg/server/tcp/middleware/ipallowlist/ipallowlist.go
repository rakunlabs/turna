package ipallowlist

import (
	"context"
	"net"
)

type IPAllowList struct {
	SourceRange []string `cfg:"source_range"`
}

func (m *IPAllowList) Middleware(ctx context.Context, _ string) (func(lconn *net.TCPConn) error, error) {
	checker, err := NewChecker(m.SourceRange)
	if err != nil {
		return nil, err
	}

	return func(lconn *net.TCPConn) error {
		if err := checker.IsAuthorized(lconn.RemoteAddr().String()); err != nil {
			return err
		}

		return nil
	}, nil
}
