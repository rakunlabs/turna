package socks5

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/things-go/go-socks5"
)

type DNSResolver struct {
	r     *net.Resolver
	ipMap map[string]string
}

func NewDNSResolver(dns string, m map[string]string) *DNSResolver {
	var r *net.Resolver
	if dns != "" {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				dial := net.Dialer{
					Timeout: time.Millisecond * time.Duration(10000),
				}

				return dial.DialContext(ctx, network, dns)
			},
		}
	}

	return &DNSResolver{
		r:     r,
		ipMap: m,
	}
}

// Resolve implement interface NameResolver
func (d *DNSResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if len(d.ipMap) > 0 {
		if strings.Index(name, ":") != -1 {
			v, _, err := net.SplitHostPort(name)
			if err != nil {
				return ctx, nil, err
			}

			name = v
		}

		// check with match
		for domain, ip := range d.ipMap {
			if ok, _ := doublestar.Match(domain, name); ok {
				slog.Debug(fmt.Sprintf("resolve %s to %s", name, ip))

				return ctx, net.ParseIP(ip), nil
			}
		}
	}

	if d.r == nil {
		addr, err := net.ResolveIPAddr("ip", name)
		if err != nil {
			return ctx, nil, err
		}

		return ctx, addr.IP, nil
	}

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
	// IPMap like {"*.google.com": "10.0.10.1"}
	IPMap map[string]string `cfg:"ip_map"`
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

	dnsResolver := NewDNSResolver(m.DNS, m.IPMap)
	opts = append(opts, socks5.WithResolver(dnsResolver))

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
