package http

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
)

const (
	// ClientAuthRequire accepts only connections with a client certificate
	// that chains to ClientCAs.
	ClientAuthRequire = "require"
	// ClientAuthRequest asks for a client certificate but accepts the
	// connection without one or with an unverified one.
	ClientAuthRequest = "request"
)

// EntryPointTLS configures the certificates and the client certificates of
// one TLS entrypoint.
type EntryPointTLS struct {
	// ClientCAs are PEM CA bundles client certificates must chain to.
	ClientCAs []string `cfg:"client_cas" log:"-"`
	// ClientAuth is require (default) or request.
	ClientAuth string `cfg:"client_auth"`
	// Store replaces the shared certificate store for this entrypoint, so
	// only these certificates are served on it. Without client_cas the
	// shared client certificate settings apply.
	Store map[string][]Certificate `cfg:"store"`
}

// config returns base with the client certificate settings of e.
func (e EntryPointTLS) config(base *tls.Config) (*tls.Config, error) {
	cfg := base.Clone()

	// an entrypoint with only its own certificates keeps the shared client
	// certificate settings of base
	if e.ClientAuth == "" && len(e.ClientCAs) == 0 && len(e.Store) > 0 {
		return cfg, nil
	}

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
	// client certificate settings of each entrypoint, see clientAuthKey
	auth map[string]string
}

// clientAuthKey identifies the client certificate settings of an
// entrypoint; connections accepted with other settings are closed.
func (c *tlsConfigs) clientAuthKey(entrypoint string) string {
	if c == nil {
		return ""
	}

	if a, ok := c.auth[entrypoint]; ok {
		return a
	}

	return c.auth[""]
}

// clientAuthID describes the client certificate settings of cfg.
func clientAuthID(cfg *tls.Config, cas []string) string {
	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "%d\x00", cfg.ClientAuth)

	for _, ca := range cas {
		_, _ = sum.Write([]byte(ca))
		_, _ = sum.Write([]byte{0})
	}

	return hex.EncodeToString(sum.Sum(nil))
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

	out := &tlsConfigs{base: base, entries: make(map[string]*tls.Config, len(h.TLS.EntryPoints)), auth: make(map[string]string)}
	out.auth[""] = clientAuthID(base, h.TLS.ClientCAFiles)

	for name, e := range h.TLS.EntryPoints {
		if _, ok := secure[name]; !ok {
			slog.Warn("tls entrypoint is not a secure entrypoint, its settings are not used", "entrypoint", name)

			continue
		}

		entryBase := base
		if len(e.Store) > 0 {
			entryBase, err = h.buildStoreTLSConfig(e.Store, false)
			if err != nil {
				return nil, fmt.Errorf("tls entrypoint %s: %w", name, err)
			}

			entryBase.NextProtos = base.NextProtos
		}

		cfg, err := e.config(entryBase)
		if err != nil {
			return nil, fmt.Errorf("tls entrypoint %s: %w", name, err)
		}

		out.entries[name] = cfg

		cas := e.ClientCAs
		if len(cas) == 0 && cfg.ClientAuth == base.ClientAuth {
			cas = h.TLS.ClientCAFiles
		}

		out.auth[name] = clientAuthID(cfg, cas)
	}

	return out, nil
}
