package tlsterminate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/cert"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// TLSTerminate performs the TLS handshake and passes the decrypted
// connection to the next middlewares.
type TLSTerminate struct {
	// Certificates to serve, selected by SNI. When empty a self-signed
	// certificate is generated.
	Certificates []Certificate `cfg:"certificates"`
	// MinVersion is "1.0", "1.1", "1.2" or "1.3", default is "1.2".
	MinVersion string `cfg:"min_version"`
	// ALPN protocols to advertise, e.g. ["h2", "http/1.1"].
	ALPN []string `cfg:"alpn"`
	// ClientCAFile enables mutual TLS with the given CA bundle.
	ClientCAFile string `cfg:"client_ca_file"`
	// ClientAuthOptional verifies client certificates only when sent.
	ClientAuthOptional bool `cfg:"client_auth_optional"`
	// HandshakeTimeout, default is 10s.
	HandshakeTimeout time.Duration `cfg:"handshake_timeout"`
}

type Certificate struct {
	CertFile string `cfg:"cert_file"`
	KeyFile  string `cfg:"key_file"`
}

func (m *TLSTerminate) TLSConfig() (*tls.Config, error) {
	cfg := &tls.Config{NextProtos: m.ALPN}

	switch strings.TrimSpace(m.MinVersion) {
	case "", "1.2":
		cfg.MinVersion = tls.VersionTLS12
	case "1.0":
		cfg.MinVersion = tls.VersionTLS10
	case "1.1":
		cfg.MinVersion = tls.VersionTLS11
	case "1.3":
		cfg.MinVersion = tls.VersionTLS13
	default:
		return nil, fmt.Errorf("unsupported min_version %q", m.MinVersion)
	}

	for _, c := range m.Certificates {
		certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot load certificate %s: %w", c.CertFile, err)
		}

		cfg.Certificates = append(cfg.Certificates, certificate)
	}

	if len(cfg.Certificates) == 0 {
		generated, err := cert.GenerateCertificateCache()
		if err != nil {
			return nil, err
		}

		certificate, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("cannot load generated certificate: %w", err)
		}

		cfg.Certificates = []tls.Certificate{certificate}
	}

	if m.ClientCAFile != "" {
		pem, err := os.ReadFile(m.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read client_ca_file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("client_ca_file has no certificates")
		}

		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert

		if m.ClientAuthOptional {
			cfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}

	return cfg, nil
}

func (m *TLSTerminate) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	cfg, err := m.TLSConfig()
	if err != nil {
		return nil, err
	}

	timeout := m.HandshakeTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return func(next tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			tlsConn := tls.Server(conn, cfg)

			hctx, cancel := context.WithTimeout(ctx, timeout)
			err := tlsConn.HandshakeContext(hctx)
			cancel()

			if err != nil {
				return fmt.Errorf("%w: tls handshake: %w", tcpmw.ErrReject, err)
			}

			return next(tlsConn)
		}
	}, nil
}
