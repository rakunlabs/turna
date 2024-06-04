package socks5

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/things-go/go-socks5"
)

type DNSResolver struct {
	r *net.Resolver
}

func NewDNSResolver(dns string) *DNSResolver {
	return &DNSResolver{
		r: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				dial := net.Dialer{
					Timeout: time.Millisecond * time.Duration(10000),
				}

				return dial.DialContext(ctx, network, dns)
			},
		},
	}
}

// Resolve implement interface NameResolver
func (d *DNSResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	ips, err := d.r.LookupIP(ctx, "ip", name)
	if err != nil {
		return ctx, nil, err
	}

	if len(ips) == 0 {
		return ctx, nil, errors.New("no ip found")
	}

	return ctx, ips[0], nil
}

type Socks5 struct {
	StaticCredentials   map[string]string `cfg:"static_credentials"`
	NoAuthAuthenticator bool              `cfg:"no_auth_authenticator"`
	DNS                 string            `cfg:"dns"`
}

func (m *Socks5) Middleware(ctx context.Context, name string) (func(lconn *net.TCPConn) error, error) {
	var authenticators []socks5.Authenticator
	if m.NoAuthAuthenticator {
		authenticators = append(authenticators, socks5.NoAuthAuthenticator{})
	}

	if len(m.StaticCredentials) > 0 {
		authenticators = append(authenticators, socks5.UserPassAuthenticator{
			Credentials: socks5.StaticCredentials(m.StaticCredentials),
		})
	}

	opts := []socks5.Option{}
	if len(authenticators) > 0 {
		opts = append(opts, socks5.WithAuthMethods(authenticators))
	}

	if m.DNS != "" {
		dnsResolver := NewDNSResolver(m.DNS)
		opts = append(opts, socks5.WithResolver(dnsResolver))
	}

	server := socks5.NewServer(opts...)

	return func(lconn *net.TCPConn) error {
		slog.Debug(fmt.Sprintf("socks5 connection from %s opened", lconn.RemoteAddr()))

		err := server.ServeConn(lconn)
		if err != nil {
			slog.Warn(fmt.Sprintf("socks5 connection from %s closed", lconn.RemoteAddr()), "error", err.Error())
		}

		return nil
	}, nil
}
