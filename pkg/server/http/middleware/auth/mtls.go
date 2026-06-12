package auth

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// clientCertFromRequest extracts the client certificate from the TLS
// handshake, or from a trusted header set by a TLS-terminating proxy
// (e.g. nginx $ssl_client_escaped_cert). The header may carry a
// URL-encoded PEM or a base64 DER certificate.
func clientCertFromRequest(r *http.Request, cfg MTLSSettings) *x509.Certificate {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		return r.TLS.PeerCertificates[0]
	}

	if cfg.CertHeader == "" {
		return nil
	}

	v := r.Header.Get(cfg.CertHeader)
	if v == "" {
		return nil
	}

	if decoded, err := url.QueryUnescape(v); err == nil {
		v = decoded
	}

	if block, _ := pem.Decode([]byte(v)); block != nil {
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			return cert
		}

		return nil
	}

	// some proxies pass base64 DER without PEM markers
	if der, err := base64.StdEncoding.DecodeString(v); err == nil {
		if cert, err := x509.ParseCertificate(der); err == nil {
			return cert
		}
	}

	return nil
}

// certFingerprint returns the lowercase hex sha256 of the DER certificate.
func certFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)

	return hex.EncodeToString(sum[:])
}

func normalizeFingerprint(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, ":", "")

	return strings.TrimPrefix(v, "sha256/")
}

// mtlsAuthenticate authenticates a service account by client certificate
// (RFC 8705 style). The service account must carry a matching
// "cert_fingerprint" (sha256) or "cert_subject" detail.
func (m *Auth) mtlsAuthenticate(r *http.Request, clientID string) (*data.UserExtended, error) {
	cfg := m.cache.Snapshot().MTLS
	if !cfg.Enabled {
		return nil, errors.New("mtls authentication is disabled")
	}

	cert := clientCertFromRequest(r, cfg)
	if cert == nil {
		return nil, errors.New("client certificate not found")
	}

	user, err := m.cache.GetUser(data.GetUserRequest{
		Alias:          clientID,
		ServiceAccount: &data.True,
		AddScopeRoles:  true,
	})
	if err != nil {
		return nil, errors.New("user not found")
	}

	if fp, _ := user.Details["cert_fingerprint"].(string); fp != "" {
		if normalizeFingerprint(fp) == certFingerprint(cert) {
			return user, nil
		}
	}

	if subject, _ := user.Details["cert_subject"].(string); subject != "" {
		if subject == cert.Subject.String() {
			return user, nil
		}
	}

	return nil, errors.New("client certificate not match")
}
