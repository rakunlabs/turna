package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

const defaultCertVerifyValue = "SUCCESS"

func parseTrustedProxy(value string) (netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix, nil
	}

	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("trusted proxy %q is not an IP address or CIDR", value)
	}

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func validateMTLSSettings(cfg MTLSSettings) error {
	if cfg.CertHeader == "" {
		// Proxy-only fields are inert in native TLS mode. Keeping them allows an
		// operator to switch modes without a transient invalid settings record.
		return nil
	}
	if cfg.CertVerifyHeader == "" {
		return errors.New("cert_verify_header is required with cert_header")
	}
	if len(cfg.TrustedProxyCIDRs) == 0 {
		return errors.New("trusted_proxy_cidrs is required with cert_header")
	}
	for _, trusted := range cfg.TrustedProxyCIDRs {
		if _, err := parseTrustedProxy(trusted); err != nil {
			return err
		}
	}

	return nil
}

func certVerifyValue(cfg MTLSSettings) string {
	if cfg.CertVerifyValue != "" {
		return cfg.CertVerifyValue
	}

	return defaultCertVerifyValue
}

func requestFromTrustedProxy(r *http.Request, trusted []string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}

	remote, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	remote = remote.Unmap()

	for _, value := range trusted {
		prefix, err := parseTrustedProxy(value)
		if err == nil && prefix.Contains(remote) {
			return true
		}
	}

	return false
}

func parseClientCertificate(v string) (*x509.Certificate, error) {
	parse := func(value string) (*x509.Certificate, error) {
		if block, _ := pem.Decode([]byte(value)); block != nil {
			if block.Type != "CERTIFICATE" {
				return nil, errors.New("client certificate PEM block has invalid type")
			}

			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse client certificate: %w", err)
			}

			return cert, nil
		}

		der, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errors.New("client certificate header is not PEM or base64 DER")
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parse client certificate: %w", err)
		}

		return cert, nil
	}

	// Try the raw value first: QueryUnescape treats '+' as a space, which
	// corrupts ordinary base64 DER. URL decoding is only a fallback for proxy
	// values such as nginx's $ssl_client_escaped_cert.
	if cert, err := parse(v); err == nil {
		return cert, nil
	}
	decoded, err := url.QueryUnescape(v)
	if err != nil || decoded == v {
		return nil, errors.New("client certificate header is not PEM or base64 DER")
	}

	return parse(decoded)
}

func validatePresentedClientCertificate(cert *x509.Certificate, now time.Time) error {
	if cert == nil {
		return errors.New("client certificate not found")
	}
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return errors.New("client certificate is expired or not yet valid")
	}
	if len(cert.ExtKeyUsage) > 0 || len(cert.UnknownExtKeyUsage) > 0 {
		clientAuth := false
		for _, usage := range cert.ExtKeyUsage {
			if usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny {
				clientAuth = true
				break
			}
		}
		if !clientAuth {
			return errors.New("client certificate is not valid for client authentication")
		}
	}

	return nil
}

// clientCertFromRequest accepts either a certificate verified by Turna's TLS
// listener or one asserted by an explicitly trusted TLS-terminating proxy.
func clientCertFromRequest(r *http.Request, cfg MTLSSettings) (*x509.Certificate, error) {
	if cfg.CertHeader == "" {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return nil, errors.New("client certificate not found")
		}
		if len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
			return nil, errors.New("client certificate was not verified by the TLS listener")
		}

		cert := r.TLS.VerifiedChains[0][0]
		if err := validatePresentedClientCertificate(cert, time.Now()); err != nil {
			return nil, err
		}

		return cert, nil
	}
	if err := validateMTLSSettings(cfg); err != nil {
		return nil, fmt.Errorf("invalid mtls proxy configuration: %w", err)
	}
	if !requestFromTrustedProxy(r, cfg.TrustedProxyCIDRs) {
		return nil, errors.New("client certificate header came from an untrusted proxy")
	}

	verified := r.Header.Get(cfg.CertVerifyHeader)
	expected := certVerifyValue(cfg)
	if subtle.ConstantTimeCompare([]byte(verified), []byte(expected)) != 1 {
		return nil, errors.New("proxy did not verify the client certificate")
	}

	v := r.Header.Get(cfg.CertHeader)
	if v == "" {
		return nil, errors.New("client certificate header not found")
	}

	cert, err := parseClientCertificate(v)
	if err != nil {
		return nil, err
	}
	if err := validatePresentedClientCertificate(cert, time.Now()); err != nil {
		return nil, err
	}

	return cert, nil
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

	cert, err := clientCertFromRequest(r, cfg)
	if err != nil {
		return nil, err
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

		// A fingerprint is an exact certificate pin. Never weaken a failed pin
		// by falling through to the less-specific subject binding.
		return nil, errors.New("client certificate not match")
	}

	if subject, _ := user.Details["cert_subject"].(string); subject != "" {
		if subject == cert.Subject.String() {
			return user, nil
		}
	}

	return nil, errors.New("client certificate not match")
}
