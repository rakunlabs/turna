package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func testClientCertificate(t *testing.T, notBefore, notAfter time.Time) (*x509.Certificate, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return cert, url.QueryEscape(string(certPEM))
}

func TestClientCertFromRequestRequiresVerifiedTLSChain(t *testing.T) {
	cert, _ := testClientCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	r := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth2/token", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	if _, err := clientCertFromRequest(r, MTLSSettings{}); err == nil {
		t.Fatal("unverified TLS peer certificate was accepted")
	}

	r.TLS.VerifiedChains = [][]*x509.Certificate{{cert}}
	got, err := clientCertFromRequest(r, MTLSSettings{})
	if err != nil || got != cert {
		t.Fatalf("verified certificate = %v, error = %v", got, err)
	}
}

func TestClientCertFromTrustedProxy(t *testing.T) {
	cert, certHeader := testClientCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cfg := MTLSSettings{
		CertHeader:        "ssl-client-cert",
		CertVerifyHeader:  "ssl-client-verify",
		TrustedProxyCIDRs: []string{"127.0.0.0/8"},
	}

	request := func(remoteAddr, verified string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://auth.example.com/oauth2/token", nil)
		r.RemoteAddr = remoteAddr
		r.Header.Set("ssl-client-cert", certHeader)
		if verified != "" {
			r.Header.Set("ssl-client-verify", verified)
		}

		return r
	}

	if _, err := clientCertFromRequest(request("203.0.113.10:1234", "SUCCESS"), cfg); err == nil {
		t.Fatal("certificate headers from an untrusted peer were accepted")
	}
	if _, err := clientCertFromRequest(request("127.0.0.1:1234", "FAILED"), cfg); err == nil {
		t.Fatal("certificate rejected by the proxy was accepted")
	}
	if _, err := clientCertFromRequest(request("127.0.0.1:1234", "SUCCESS"), cfg); err != nil {
		t.Fatalf("verified trusted-proxy certificate rejected: %v", err)
	}

	rawBase64 := base64.StdEncoding.EncodeToString(cert.Raw)
	if !strings.Contains(rawBase64, "+") {
		t.Fatal("test certificate base64 unexpectedly contains no plus character")
	}
	r := request("127.0.0.1:1234", "SUCCESS")
	r.Header.Set("ssl-client-cert", rawBase64)
	if _, err := clientCertFromRequest(r, cfg); err != nil {
		t.Fatalf("raw base64 DER certificate rejected: %v", err)
	}
}

func TestClientCertFromRequestRejectsExpiredCertificate(t *testing.T) {
	_, certHeader := testClientCertificate(t, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	cfg := MTLSSettings{
		CertHeader:        "ssl-client-cert",
		CertVerifyHeader:  "ssl-client-verify",
		TrustedProxyCIDRs: []string{"127.0.0.1"},
	}
	r := httptest.NewRequest(http.MethodPost, "http://auth.example.com/oauth2/token", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("ssl-client-cert", certHeader)
	r.Header.Set("ssl-client-verify", "SUCCESS")

	if _, err := clientCertFromRequest(r, cfg); err == nil {
		t.Fatal("expired client certificate was accepted")
	}
}

func TestValidatePresentedClientCertificateRejectsUnknownEKU(t *testing.T) {
	cert, _ := testClientCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cert.ExtKeyUsage = nil
	cert.UnknownExtKeyUsage = []asn1.ObjectIdentifier{{1, 2, 3, 4}}

	if err := validatePresentedClientCertificate(cert, time.Now()); err == nil {
		t.Fatal("certificate restricted to an unknown EKU was accepted")
	}
}

func TestMTLSFingerprintTakesPrecedenceOverSubject(t *testing.T) {
	pinned, _ := testClientCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	presented, _ := testClientCertificate(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	service := &data.User{
		ID:             "service",
		Alias:          []string{"service"},
		ServiceAccount: true,
		Details: map[string]any{
			"cert_fingerprint": certFingerprint(pinned),
			"cert_subject":     presented.Subject.String(),
		},
	}
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{
		Users: map[string]*data.User{service.ID: service},
		Alias: map[string]string{"service": service.ID},
		MTLS:  MTLSSettings{Enabled: true},
	})
	m := &Auth{cache: cache}
	r := httptest.NewRequest(http.MethodPost, "https://auth.example.com/oauth2/token", nil)
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{presented},
		VerifiedChains:   [][]*x509.Certificate{{presented}},
	}

	if _, err := m.mtlsAuthenticate(r, "service"); err == nil {
		t.Fatal("subject match bypassed a configured fingerprint pin")
	}
}

func TestValidateMTLSSettings(t *testing.T) {
	invalid := []MTLSSettings{
		{CertHeader: "ssl-client-cert"},
		{CertHeader: "ssl-client-cert", CertVerifyHeader: "ssl-client-verify"},
		{CertHeader: "ssl-client-cert", CertVerifyHeader: "ssl-client-verify", TrustedProxyCIDRs: []string{"not-an-ip"}},
	}
	for _, cfg := range invalid {
		if err := validateMTLSSettings(cfg); err == nil {
			t.Errorf("invalid settings accepted: %+v", cfg)
		}
	}

	valid := MTLSSettings{
		CertHeader:        "ssl-client-cert",
		CertVerifyHeader:  "ssl-client-verify",
		TrustedProxyCIDRs: []string{"10.0.0.0/8", "127.0.0.1"},
	}
	if err := validateMTLSSettings(valid); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	if err := validateMTLSSettings(MTLSSettings{CertVerifyValue: "SUCCESS"}); err != nil {
		t.Fatalf("native settings with inert proxy fields rejected: %v", err)
	}
}
