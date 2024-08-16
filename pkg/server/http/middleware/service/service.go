package service

import (
	"fmt"
	"net/http"
	"net/url"

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

func (m *Service) GetBalancer() (ProxyBalancer, error) {
	if m.PrefixBalancer.IsEnabled() {
		for i, prefix := range m.PrefixBalancer.Prefixes {
			targets := make([]*ProxyTarget, 0, len(prefix.Servers))

			for _, server := range prefix.Servers {
				u, err := url.Parse(server.URL)
				if err != nil {
					return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
				}

				targets = append(targets, &ProxyTarget{
					URL: u,
				})
			}

			m.PrefixBalancer.Prefixes[i].Balancer = NewRoundRobinBalancer(targets)
		}

		if len(m.PrefixBalancer.DefaultServers) > 0 {
			targets := make([]*ProxyTarget, 0, len(m.PrefixBalancer.DefaultServers))

			for _, server := range m.PrefixBalancer.DefaultServers {
				u, err := url.Parse(server.URL)
				if err != nil {
					return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
				}

				targets = append(targets, &ProxyTarget{URL: u})
			}

			m.PrefixBalancer.DefaultBalancer = NewRoundRobinBalancer(targets)
		}

		return &m.PrefixBalancer, nil
	}

	targets := make([]*ProxyTarget, 0, len(m.LoadBalancer.Servers))

	for _, server := range m.LoadBalancer.Servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
		}

		targets = append(targets, &ProxyTarget{
			URL: u,
		})
	}

	return NewRoundRobinBalancer(targets), nil
}

func (m *Service) Middleware() ([]func(http.Handler) http.Handler, error) {
	cfg := DefaultProxyConfig
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

	checkHost := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !m.PassHostHeader {
				r.Host = ""
			}

			next.ServeHTTP(w, r)
		})
	}

	return []func(http.Handler) http.Handler{
		checkHost,
		ProxyWithConfig(cfg),
	}, nil
}
