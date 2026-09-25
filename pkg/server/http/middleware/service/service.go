package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type Service struct {
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`
	PassHostHeader     *bool  `cfg:"pass_host_header"`
	Proxy              string `cfg:"proxy"`

	PrefixBalancer PrefixBalancer `cfg:"prefixbalancer"`
	LoadBalancer   LoadBalancer   `cfg:"loadbalancer"`

	// Retry is the number of retries on other upstreams when an upstream is unreachable.
	Retry int `cfg:"retry"`

	Mirror *Mirror `cfg:"mirror"`
}

type LoadBalancer struct {
	Servers []Server `cfg:"servers"`
	// Strategy is round_robin, random or least_conn; default is round_robin.
	Strategy string `cfg:"strategy"`
	// Sticky enables session affinity.
	Sticky *Sticky `cfg:"sticky"`

	HealthCheck        *HealthCheck        `cfg:"health_check"`
	PassiveHealthCheck *PassiveHealthCheck `cfg:"passive_health_check"`
}

type balancerBuilder struct {
	states  map[string]*targetState
	health  *healthChecker
	passive *PassiveHealthCheck
}

func (b *balancerBuilder) targets(servers []Server) ([]*ProxyTarget, error) {
	targets := make([]*ProxyTarget, 0, len(servers))

	for _, server := range servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			return nil, fmt.Errorf("cannot parse url %s: %w", server.URL, err)
		}

		if server.Weight < 0 {
			return nil, fmt.Errorf("weight of %s must not be negative", server.URL)
		}

		// same upstream in different groups shares health and connection state
		state, ok := b.states[u.String()]
		if !ok {
			state = newTargetState(u, b.passive)
			b.states[u.String()] = state
		}

		targets = append(targets, &ProxyTarget{
			Name:   u.String(),
			URL:    u,
			Weight: server.Weight,
			state:  state,
		})
	}

	return targets, nil
}

func (b *balancerBuilder) balancer(servers []Server, cookieSuffix string, lb *LoadBalancer) (ProxyBalancer, error) {
	targets, err := b.targets(servers)
	if err != nil {
		return nil, err
	}

	sticky, err := lb.Sticky.build(cookieSuffix)
	if err != nil {
		return nil, err
	}

	return newLBBalancer(targets, lb.Strategy, sticky)
}

func (m *Service) GetBalancer() (ProxyBalancer, error) {
	balancer, _, err := m.getBalancer(nil)

	return balancer, err
}

func (m *Service) getBalancer(transport http.RoundTripper) (ProxyBalancer, *balancerBuilder, error) {
	lb := &m.LoadBalancer

	builder := &balancerBuilder{
		states:  make(map[string]*targetState),
		passive: lb.PassiveHealthCheck,
	}

	if lb.HealthCheck != nil {
		health, err := newHealthChecker(lb.HealthCheck, transport)
		if err != nil {
			return nil, nil, err
		}

		builder.health = health
	}

	if m.PrefixBalancer.IsEnabled() {
		for i, prefix := range m.PrefixBalancer.Prefixes {
			balancer, err := builder.balancer(prefix.Servers, "_"+strconv.Itoa(i), lb)
			if err != nil {
				return nil, nil, err
			}

			m.PrefixBalancer.Prefixes[i].Balancer = balancer
		}

		if len(m.PrefixBalancer.DefaultServers) > 0 {
			balancer, err := builder.balancer(m.PrefixBalancer.DefaultServers, "", lb)
			if err != nil {
				return nil, nil, err
			}

			m.PrefixBalancer.DefaultBalancer = balancer
		}

		return &m.PrefixBalancer, builder, nil
	}

	balancer, err := builder.balancer(lb.Servers, "", lb)
	if err != nil {
		return nil, nil, err
	}

	return balancer, builder, nil
}

func (m *Service) Middleware(ctx context.Context) ([]func(http.Handler) http.Handler, error) {
	cfg := DefaultProxyConfig

	// Dedicated transport for the reverse proxy. A plain *http.Transport is
	// required so that httputil.ReverseProxy can natively proxy WebSocket /
	// Upgrade requests (the 101 response body is returned as an
	// io.ReadWriteCloser) and so that upstream TLS (https/wss) is handled here.
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}

	transport = transport.Clone()
	if m.Proxy != "" {
		proxyURL, err := url.Parse(m.Proxy)
		if err != nil {
			return nil, fmt.Errorf("cannot parse proxy url %s: %w", m.Proxy, err)
		}
		if proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return nil, fmt.Errorf("proxy url %s must use http or https", m.Proxy)
		}

		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if m.InsecureSkipVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{} //nolint:gosec // opt-in skip verify
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	cfg.Transport = transport

	balancer, builder, err := m.getBalancer(transport)
	if err != nil {
		return nil, fmt.Errorf("cannot get balancer: %w", err)
	}

	cfg.Balancer = balancer
	cfg.RetryCount = m.Retry
	cfg.passive = m.LoadBalancer.PassiveHealthCheck

	if builder.health != nil {
		for rawURL, state := range builder.states {
			u, _ := url.Parse(rawURL)
			go builder.health.run(ctx, u, state)
		}
	}

	mirror, err := m.Mirror.build(transport)
	if err != nil {
		return nil, fmt.Errorf("cannot create mirror: %w", err)
	}

	checkHost := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.PassHostHeader != nil && !(*m.PassHostHeader) {
				r.Host = ""
			}

			next.ServeHTTP(w, r)
		})
	}

	middlewares := []func(http.Handler) http.Handler{checkHost}
	if mirror != nil {
		middlewares = append(middlewares, mirror.Middleware)
	}

	return append(middlewares, ProxyWithConfig(cfg)), nil
}
