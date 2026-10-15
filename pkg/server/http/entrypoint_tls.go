package http

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

const (
	// ClientAuthRequire accepts only connections with a client certificate
	// that chains to ClientCAs.
	ClientAuthRequire = "require"
	// ClientAuthRequest asks for a client certificate but accepts the
	// connection without one or with an unverified one.
	ClientAuthRequest = "request"
)

// EntryPointTLS configures client certificates of one TLS entrypoint.
type EntryPointTLS struct {
	// ClientCAs are PEM CA bundles client certificates must chain to.
	ClientCAs []string `cfg:"client_cas" log:"-"`
	// ClientAuth is require (default) or request.
	ClientAuth string `cfg:"client_auth"`
}

// config returns base with the client certificate settings of e.
func (e EntryPointTLS) config(base *tls.Config) (*tls.Config, error) {
	cfg := base.Clone()

	switch e.ClientAuth {
	case "", ClientAuthRequire:
		if len(e.ClientCAs) == 0 {
			return nil, fmt.Errorf("client_cas is required with client_auth %s", ClientAuthRequire)
		}

		pool := x509.NewCertPool()
		for i, ca := range e.ClientCAs {
			if !pool.AppendCertsFromPEM([]byte(ca)) {
				return nil, fmt.Errorf("client_cas[%d] contains no certificates", i)
			}
		}

		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = pool
	case ClientAuthRequest:
		// certificates are not verified, the backend decides
		cfg.ClientAuth = tls.RequestClientCert
		cfg.ClientCAs = nil
	default:
		return nil, fmt.Errorf("unsupported client_auth %q", e.ClientAuth)
	}

	return cfg, nil
}

// tlsConfigs are the TLS configurations of the secure entrypoints.
type tlsConfigs struct {
	base    *tls.Config
	entries map[string]*tls.Config
}

func (c *tlsConfigs) forEntrypoint(entrypoint string) *tls.Config {
	if cfg, ok := c.entries[entrypoint]; ok {
		return cfg
	}

	return c.base
}

// buildTLSConfigs builds the shared configuration and the entrypoint
// overrides for the given secure entrypoints.
func (h *HTTP) buildTLSConfigs(secure map[string]struct{}) (*tlsConfigs, error) {
	base, err := h.buildTLSConfig()
	if err != nil {
		return nil, err
	}

	// GetConfigForClient bypasses the HTTP/2 setup of ServeTLS, so the
	// returned config must advertise ALPN protocols itself.
	if len(base.NextProtos) == 0 {
		base.NextProtos = []string{"h2", "http/1.1"}
	}

	out := &tlsConfigs{base: base, entries: make(map[string]*tls.Config, len(h.TLS.EntryPoints))}

	for name, e := range h.TLS.EntryPoints {
		if _, ok := secure[name]; !ok {
			continue
		}

		cfg, err := e.config(base)
		if err != nil {
			return nil, fmt.Errorf("tls entrypoint %s: %w", name, err)
		}

		out.entries[name] = cfg
	}

	return out, nil
}
