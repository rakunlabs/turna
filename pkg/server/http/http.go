package http

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mholt/acmez/v3"

	"github.com/rakunlabs/turna/pkg/server/acme"
	"github.com/rakunlabs/turna/pkg/server/cert"
)

type HTTP struct {
	Routers     map[string]Router         `cfg:"routers"`
	Middlewares map[string]HTTPMiddleware `cfg:"middlewares"`
	TLS         TLS                       `cfg:"tls"`
}

type TLS struct {
	// Store maps an SNI host name to its certificate(s). The special key
	// "default" is used as the fallback when the client sends no SNI server
	// name or no host entry matches.
	Store map[string][]Certificate `cfg:"store"`
	// MinVersion is the minimum accepted TLS version: "1.2" or "1.3".
	// Defaults to "1.3".
	MinVersion string `cfg:"min_version"`
	// ClientCAFiles contains PEM CA bundles used to verify optional client
	// certificates. When set, the listener requests a client certificate and
	// rejects any presented certificate that does not chain to these roots.
	ClientCAFiles []string `cfg:"client_ca_files"`
	// EntryPoints overrides client certificate validation per entrypoint.
	EntryPoints map[string]EntryPointTLS `cfg:"entrypoints"`
	// SelfSigned customizes the auto-generated certificate used when no
	// certificate is configured in Store.
	SelfSigned SelfSigned `cfg:"self_signed"`
	// ACME enables automatic certificate provisioning from an ACME CA such as
	// Let's Encrypt.
	ACME *ACME `cfg:"acme"`
}

// ACME configures automatic certificate provisioning from an ACME CA such as
// Let's Encrypt with the HTTP-01, TLS-ALPN-01 or DNS-01 challenge.
type ACME = acme.ACME

type SelfSigned struct {
	Organization []string `cfg:"organization"`
	DNSNames     []string `cfg:"dns_names"`
	IPs          []string `cfg:"ips"`
}

type Certificate struct {
	CertFile string `cfg:"cert_file"`
	KeyFile  string `cfg:"key_file"`
	// Cert and Key hold PEM encoded content and take precedence over the
	// file fields, used by dynamic providers such as kubernetes secrets.
	Cert string `cfg:"cert" log:"-"`
	Key  string `cfg:"key"  log:"-"`
}

func (c Certificate) load() (tls.Certificate, error) {
	if c.Cert != "" || c.Key != "" {
		return tls.X509KeyPair([]byte(c.Cert), []byte(c.Key))
	}

	return tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
}

// minVersion converts the configured MinVersion string to a tls constant.
// Default is TLS 1.3.
func (t TLS) minVersion() (uint16, error) {
	switch t.MinVersion {
	case "", "1.3", "13", "tls1.3":
		return tls.VersionTLS13, nil
	case "1.2", "12", "tls1.2":
		return tls.VersionTLS12, nil
	default:
		return 0, fmt.Errorf("unsupported tls min_version %q (use \"1.2\" or \"1.3\")", t.MinVersion)
	}
}

// buildTLSConfig builds an SNI-aware *tls.Config from the configured Store.
// Every Store host key is loaded, and GetCertificate selects a certificate by
// the client's SNI server name (exact, then wildcard), falling back to the
// "default" host key and finally to a generated self-signed certificate.
func (h *HTTP) buildTLSConfig() (*tls.Config, error) {
	clientCAs, _, err := h.loadClientCAs()
	if err != nil {
		return nil, err
	}

	return h.buildStoreTLSConfig(h.TLS.Store, true, nil, clientCAs)
}

// loadClientCAs reads ClientCAFiles and returns their pool and PEM contents,
// nil without files.
func (h *HTTP) loadClientCAs() (*x509.CertPool, []string, error) {
	if len(h.TLS.ClientCAFiles) == 0 {
		return nil, nil, nil
	}

	pool := x509.NewCertPool()
	pems := make([]string, 0, len(h.TLS.ClientCAFiles))

	for _, file := range h.TLS.ClientCAFiles {
		caPEM, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot read client CA file %q: %w", file, err)
		}
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, nil, fmt.Errorf("client CA file %q contains no certificates", file)
		}

		pems = append(pems, string(caPEM))
	}

	return pool, pems, nil
}

// buildStoreTLSConfig builds an SNI-aware *tls.Config serving the
// certificates of store. shared adds ACME of the shared listener
// configuration; a non-nil clientCAs verifies optional client certificates.
func (h *HTTP) buildStoreTLSConfig(store map[string][]Certificate, shared bool, acmeManager *acme.Manager, clientCAs *x509.CertPool) (*tls.Config, error) {
	minVersion, err := h.TLS.minVersion()
	if err != nil {
		return nil, err
	}

	byHost := make(map[string]tls.Certificate, len(store))
	allCerts := make([]tls.Certificate, 0, len(store))

	// sorted so the first certificate of a host and the fallback are stable
	for _, key := range slices.Sorted(maps.Keys(store)) {
		certList := store[key]
		host := strings.TrimSuffix(strings.ToLower(key), ".")

		for _, c := range certList {
			certificate, err := c.load()
			if err != nil {
				return nil, fmt.Errorf("cannot load certificate for host %q: %w, certFile: %s, keyFile: %s", host, err, c.CertFile, c.KeyFile)
			}

			// first certificate wins per host key
			if _, ok := byHost[host]; !ok {
				byHost[host] = certificate
			}
			allCerts = append(allCerts, certificate)
		}
	}

	// Determine the fallback certificate.
	var fallback tls.Certificate
	switch {
	case len(byHost["default"].Certificate) > 0:
		fallback = byHost["default"]
	case len(allCerts) > 0:
		fallback = allCerts[0]
	default:
		generated, err := h.generateSelfSigned()
		if err != nil {
			return nil, err
		}
		fallback = generated
		allCerts = append(allCerts, generated)
	}

	if !shared {
		acmeManager = nil
	}

	cfg := &tls.Config{
		MinVersion:   minVersion,
		Certificates: allCerts,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// A TLS-ALPN-01 challenge handshake must always be answered by the
			// ACME manager, even when a static certificate is configured for the
			// same host, otherwise validation fails.
			if acmeManager != nil && acme.IsChallenge(hello) {
				return acmeManager.GetCertificate(hello)
			}

			if serverName := strings.TrimSuffix(strings.ToLower(hello.ServerName), "."); serverName != "" {
				if certificate, ok := byHost[serverName]; ok {
					return &certificate, nil
				}
				if certificate, ok := wildcardCert(byHost, serverName); ok {
					return certificate, nil
				}
			}

			// No statically configured certificate matched; use the ACME
			// certificate of the host, obtained on demand when allowed.
			if acmeManager != nil {
				certificate, err := acmeManager.GetCertificate(hello)
				if err == nil {
					return certificate, nil
				}
				slog.Debug("acme could not provide certificate, using fallback",
					"server_name", hello.ServerName, "err", err.Error())
			}

			return &fallback, nil
		},
	}

	if clientCAs != nil {
		// mTLS is optional at the shared listener: routes that require it check
		// VerifiedChains, while ordinary HTTPS routes continue to work.
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
		cfg.ClientCAs = clientCAs
	}

	if acmeManager != nil && acmeManager.TLSALPN() {
		// Advertise the ACME TLS-ALPN-01 protocol while preserving normal
		// HTTP/2 and HTTP/1.1 negotiation for regular traffic.
		cfg.NextProtos = append(cfg.NextProtos, "h2", "http/1.1", acmez.ACMETLS1Protocol)
	}

	return cfg, nil
}

// wildcardCert looks up a certificate for serverName with the longest
// wildcard host, like the router does: "*.example.com" matches
// "api.example.com" and "a.b.example.com".
func wildcardCert(byHost map[string]tls.Certificate, serverName string) (*tls.Certificate, bool) {
	for i := strings.IndexByte(serverName, '.'); i > 0; {
		if certificate, ok := byHost["*"+serverName[i:]]; ok {
			return &certificate, true
		}

		next := strings.IndexByte(serverName[i+1:], '.')
		if next < 0 {
			break
		}

		i += next + 1
	}

	return nil, false
}

// generateSelfSigned builds a cached self-signed certificate, honoring the
// optional SelfSigned config.
func (h *HTTP) generateSelfSigned() (tls.Certificate, error) {
	opts := make([]cert.Options, 0, 3)
	if len(h.TLS.SelfSigned.Organization) > 0 {
		opts = append(opts, cert.WithOrganization(h.TLS.SelfSigned.Organization...))
	}
	if len(h.TLS.SelfSigned.DNSNames) > 0 {
		opts = append(opts, cert.WithDNSNames(h.TLS.SelfSigned.DNSNames...))
	}
	if len(h.TLS.SelfSigned.IPs) > 0 {
		opts = append(opts, cert.WithIPs(h.TLS.SelfSigned.IPs...))
	}

	generated, err := cert.GenerateCertificateCache(opts...)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot generate self-signed certificate: %w", err)
	}

	certificate, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot load generated certificate: %w", err)
	}

	return certificate, nil
}

var ReadHeaderTimeout = 10 * time.Second

// IdleTimeout is the keep-alive timeout of entrypoints without idle_timeout.
var IdleTimeout = 120 * time.Second
