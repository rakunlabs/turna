package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Service struct {
	InsecureSkipVerify bool   `cfg:"insecure_skip_verify"`
	PassHostHeader     *bool  `cfg:"pass_host_header"`
	Proxy              string `cfg:"proxy"`

	PrefixBalancer PrefixBalancer `cfg:"prefixbalancer"`
	LoadBalancer   LoadBalancer   `cfg:"loadbalancer"`

	// Retry is the number of retries on other upstreams when an upstream is unreachable.
	Retry int `cfg:"retry"`

	// H2C connects to http upstreams with HTTP/2 prior knowledge, required
	// for gRPC backends without TLS.
	H2C bool `cfg:"h2c"`

	// TLS verifies https upstreams against a CA bundle instead of the system
	// roots, with a fixed server name.
	TLS *UpstreamTLS `cfg:"tls"`

	Mirror *Mirror `cfg:"mirror"`

	// Timeouts of upstream connections, 0 keeps the defaults.
	Timeouts Timeouts `cfg:"timeouts"`
}

type Timeouts struct {
	// Dial bounds establishing the TCP connection, default is 30s.
	Dial time.Duration `cfg:"dial"`
	// TLSHandshake bounds the TLS handshake, default is 10s.
	TLSHandshake time.Duration `cfg:"tls_handshake"`
	// ResponseHeader bounds waiting for the response headers after the
	// request is sent, default is no limit.
	ResponseHeader time.Duration `cfg:"response_header"`
	// IdleConn closes idle keep-alive connections, default is 90s.
	IdleConn time.Duration `cfg:"idle_conn"`
}

func (t Timeouts) apply(transport *http.Transport) error {
	for name, d := range map[string]time.Duration{
		"dial": t.Dial, "tls_handshake": t.TLSHandshake, "response_header": t.ResponseHeader, "idle_conn": t.IdleConn,
	} {
		if d < 0 {
			return fmt.Errorf("timeouts.%s must not be negative", name)
		}
	}

	if t.Dial > 0 {
		transport.DialContext = (&net.Dialer{Timeout: t.Dial, KeepAlive: 30 * time.Second}).DialContext
	}
	if t.TLSHandshake > 0 {
		transport.TLSHandshakeTimeout = t.TLSHandshake
	}
	if t.ResponseHeader > 0 {
		transport.ResponseHeaderTimeout = t.ResponseHeader
	}
	if t.IdleConn > 0 {
		transport.IdleConnTimeout = t.IdleConn
	}

	return nil
}

// UpstreamTLS configures verification of https upstreams.
type UpstreamTLS struct {
	// CA is a PEM bundle of trusted CA certificates. Empty uses the system
	// roots.
	CA string `cfg:"ca"`
	// ServerName is sent as SNI and verified against the certificate unless
	// SubjectAltNames is set.
	ServerName string `cfg:"server_name"`
	// SubjectAltNames replace ServerName for verification; the certificate
	// must contain one of them. DNS names may use a leading "*." wildcard,
	// values with a scheme are matched against URI SANs.
	SubjectAltNames []string `cfg:"subject_alt_names"`
	// Invalid makes every connection fail, used when the configured trust
	// anchors cannot be loaded.
	Invalid bool `cfg:"invalid"`
	// ClientCert and ClientKey are a PEM certificate and key presented to the
	// upstream when it asks for a client certificate.
	ClientCert string `cfg:"client_cert" log:"-"`
	ClientKey  string `cfg:"client_key"  log:"-"`
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
	sockets unixSockets
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

		name := u.String()
		if isUnixURL(u) {
			if u, err = b.sockets.rewrite(server.URL, u); err != nil {
				return nil, err
			}
		}

		// same upstream in different groups shares health and connection state
		state, ok := b.states[u.String()]
		if !ok {
			state = newTargetState(u, b.passive)
			b.states[u.String()] = state
		}

		targets = append(targets, &ProxyTarget{
			Name:   name,
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
		sockets: make(unixSockets),
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
	if err := m.Timeouts.apply(transport); err != nil {
		return nil, err
	}
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
	if m.TLS != nil {
		tlsConfig, err := m.TLS.config()
		if err != nil {
			return nil, err
		}

		transport.TLSClientConfig = tlsConfig
	}
	if m.H2C {
		var protocols http.Protocols
		protocols.SetUnencryptedHTTP2(true)
		protocols.SetHTTP2(true)
		transport.Protocols = &protocols
	}

	cfg.Transport = transport

	balancer, builder, err := m.getBalancer(transport)
	if err != nil {
		return nil, fmt.Errorf("cannot get balancer: %w", err)
	}

	cfg.Balancer = balancer
	cfg.RetryCount = m.Retry
	cfg.passive = m.LoadBalancer.PassiveHealthCheck

	mirrorTransport := transport
	if m.Mirror != nil && m.Mirror.TLS != nil {
		tlsConfig, err := m.Mirror.TLS.config()
		if err != nil {
			return nil, err
		}

		mirrorTransport = transport.Clone()
		mirrorTransport.TLSClientConfig = tlsConfig
	}

	mirror, err := m.Mirror.build(mirrorTransport, builder.sockets)
	if err != nil {
		return nil, fmt.Errorf("cannot create mirror: %w", err)
	}

	builder.sockets.wrap(transport)
	if mirrorTransport != transport {
		builder.sockets.wrap(mirrorTransport)
	}

	if builder.health != nil {
		for rawURL, state := range builder.states {
			u, _ := url.Parse(rawURL)
			go builder.health.run(ctx, u, state)
		}
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
