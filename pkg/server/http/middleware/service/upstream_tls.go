package service

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var errInvalidUpstreamTLS = errors.New("upstream tls configuration is invalid")

// config builds the client TLS configuration. Verification is done in
// VerifyConnection so a fixed server name and subject alt names apply
// regardless of the upstream address.
func (t *UpstreamTLS) config() (*tls.Config, error) {
	if t.Invalid {
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: t.ServerName,
			// the handshake must fail before any request is sent
			InsecureSkipVerify: true, //nolint:gosec // verified below and always rejected
			VerifyConnection: func(tls.ConnectionState) error {
				return errInvalidUpstreamTLS
			},
		}, nil
	}

	var roots *x509.CertPool

	if t.CA != "" {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(t.CA)) {
			return nil, errors.New("upstream tls: ca contains no certificates")
		}
	}

	sans := t.SubjectAltNames

	var certificates []tls.Certificate
	if t.ClientCert != "" || t.ClientKey != "" {
		c, err := tls.X509KeyPair([]byte(t.ClientCert), []byte(t.ClientKey))
		if err != nil {
			return nil, fmt.Errorf("upstream tls: client certificate: %w", err)
		}

		certificates = []tls.Certificate{c}
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   t.ServerName,
		Certificates: certificates,
		// the default verification uses the dialed host, which is a pod
		// address; VerifyConnection checks the chain and names instead
		InsecureSkipVerify: true, //nolint:gosec // verified in VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("upstream tls: no certificate presented")
			}

			opts := x509.VerifyOptions{
				Roots:         roots,
				Intermediates: x509.NewCertPool(),
			}
			for _, c := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(c)
			}

			leaf := cs.PeerCertificates[0]
			if _, err := leaf.Verify(opts); err != nil {
				return fmt.Errorf("upstream tls: %w", err)
			}

			if len(sans) == 0 {
				// without a configured name the dialed host is verified,
				// which the transport sends as the server name
				name := t.ServerName
				if name == "" {
					name = cs.ServerName
				}

				if name == "" {
					return errors.New("upstream tls: no server name to verify")
				}

				return leaf.VerifyHostname(name)
			}

			for _, san := range sans {
				if matchSAN(leaf, san) {
					return nil
				}
			}

			return fmt.Errorf("upstream tls: certificate matches none of the subject alt names %v", sans)
		},
	}, nil
}

func matchSAN(leaf *x509.Certificate, san string) bool {
	if strings.Contains(san, "://") {
		for _, u := range leaf.URIs {
			if u.String() == san {
				return true
			}
		}

		if want, err := url.Parse(san); err == nil {
			for _, u := range leaf.URIs {
				if *u == *want {
					return true
				}
			}
		}

		return false
	}

	// VerifyHostname matches DNS SANs, including wildcards in the
	// certificate; a wildcard in the configured name matches one label.
	if leaf.VerifyHostname(san) == nil {
		return true
	}

	if rest, ok := strings.CutPrefix(san, "*."); ok {
		for _, name := range leaf.DNSNames {
			if _, domain, found := strings.Cut(name, "."); found && strings.EqualFold(domain, rest) && !strings.HasPrefix(name, "*") {
				return true
			}
		}
	}

	return false
}
