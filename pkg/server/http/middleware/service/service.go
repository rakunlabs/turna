package service

import (
	"fmt"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/worldline-go/klient"
)

type Service struct {
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
	PassHostHeader     bool `cfg:"pass_host_header"`

	PrefixBalancer PrefixBalancer `cfg:"prefixbalancer"`
	LoadBalancer   LoadBalancer   `cfg:"loadbalancer"`
}

type LoadBalancer struct {
	Servers []Server `cfg:"servers"`
}

type Server struct {
	URL string `cfg:"url"`
}

func (m *Service) GetBalancer() (middleware.ProxyBalancer, error) {
	if m.PrefixBalancer.IsEnabled() {
		for i, prefix := range m.PrefixBalancer.Prefixes {
			targets := make([]*middleware.ProxyTarget, 0, len(prefix.Servers))

			for _, server := range prefix.Servers {
				u, err := url.Parse(server.URL)
				if err != nil {
					return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
				}

				targets = append(targets, &middleware.ProxyTarget{
					URL: u,
				})
			}

			m.PrefixBalancer.Prefixes[i].balancer = middleware.NewRoundRobinBalancer(targets)
		}

		if len(m.PrefixBalancer.DefaultServers) > 0 {
			targets := make([]*middleware.ProxyTarget, 0, len(m.PrefixBalancer.DefaultServers))

			for _, server := range m.PrefixBalancer.DefaultServers {
				u, err := url.Parse(server.URL)
				if err != nil {
					return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
				}

				targets = append(targets, &middleware.ProxyTarget{URL: u})
			}

			m.PrefixBalancer.defaultBalancer = middleware.NewRoundRobinBalancer(targets)
		}

		return &m.PrefixBalancer, nil
	}

	targets := make([]*middleware.ProxyTarget, 0, len(m.LoadBalancer.Servers))

	for _, server := range m.LoadBalancer.Servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
		}

		targets = append(targets, &middleware.ProxyTarget{
			URL: u,
		})
	}

	return middleware.NewRoundRobinBalancer(targets), nil
}

func (m *Service) Middleware() ([]echo.MiddlewareFunc, error) {
	cfg := middleware.DefaultProxyConfig
	balancer, err := m.GetBalancer()
	if err != nil {
		return nil, fmt.Errorf("cannot get balancer: %w", err)
	}

	cfg.Balancer = balancer

	client, err := klient.NewPlain(
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	cfg.Transport = client.HTTP.Transport

	checkHost := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !m.PassHostHeader {
				c.Request().Host = ""
			}

			return next(c)
		}
	}

	return []echo.MiddlewareFunc{checkHost, middleware.ProxyWithConfig(cfg)}, nil
}
