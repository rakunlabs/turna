package http

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/cert"
)

func writeCertFiles(t *testing.T, marker string) Certificate {
	t.Helper()

	c, err := cert.GenerateCertificate(cert.WithDNSNames(marker))
	if err != nil {
		t.Fatalf("generate certificate: %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certFile, c.Certificate, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, c.PrivateKey, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return Certificate{CertFile: certFile, KeyFile: keyFile}
}

func leafDNS(t *testing.T, c *tls.Certificate) []string {
	t.Helper()

	if c == nil || len(c.Certificate) == 0 {
		t.Fatal("nil certificate")
	}

	x, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	return x.DNSNames
}

func TestBuildTLSConfigSNI(t *testing.T) {
	h := &HTTP{
		TLS: TLS{
			Store: map[string][]Certificate{
				"app.example.com":        {writeCertFiles(t, "app.example.com")},
				"*.internal.example.com": {writeCertFiles(t, "wildcard.internal.example.com")},
				"default":                {writeCertFiles(t, "default.example.com")},
			},
		},
	}

	cfg, err := h.buildTLSConfig()
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected MinVersion TLS 1.3, got %x", cfg.MinVersion)
	}

	tests := []struct {
		serverName string
		wantDNS    string
	}{
		{"app.example.com", "app.example.com"},
		{"api.internal.example.com", "wildcard.internal.example.com"},
		{"unknown.host", "default.example.com"},
		{"", "default.example.com"},
	}

	for _, tt := range tests {
		c, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: tt.serverName})
		if err != nil {
			t.Fatalf("GetCertificate(%q): %v", tt.serverName, err)
		}

		if dns := leafDNS(t, c); !slices.Contains(dns, tt.wantDNS) {
			t.Errorf("serverName %q: expected cert with DNS %q, got %v", tt.serverName, tt.wantDNS, dns)
		}
	}
}

func TestBuildTLSConfigClientCAs(t *testing.T) {
	clientCA := writeCertFiles(t, "client-ca.example.com")
	h := &HTTP{TLS: TLS{ClientCAFiles: []string{clientCA.CertFile}}}

	cfg, err := h.buildTLSConfig()
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if cfg.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Fatalf("ClientAuth = %v, want VerifyClientCertIfGiven", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs is nil")
	}
	if len(cfg.ClientCAs.Subjects()) != 1 {
		t.Fatalf("ClientCAs subjects = %d", len(cfg.ClientCAs.Subjects()))
	}
}

func testClientPKI(t *testing.T) (string, func(time.Time, time.Time) tls.Certificate) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-client-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caFile := filepath.Join(t.TempDir(), "client-ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	issue := func(notBefore, notAfter time.Time) tls.Certificate {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		template := x509.Certificate{
			SerialNumber: big.NewInt(2),
			Subject:      pkix.Name{CommonName: "test-client"},
			NotBefore:    notBefore,
			NotAfter:     notAfter,
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, &template, &caTemplate, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		certificate, err := tls.X509KeyPair(append(leafPEM, caPEM...), keyPEM)
		if err != nil {
			t.Fatal(err)
		}

		return certificate
	}

	return caFile, issue
}

func TestTLSClientCertificateHandshake(t *testing.T) {
	trustedCA, issueTrusted := testClientPKI(t)
	_, issueUnknown := testClientPKI(t)
	h := &HTTP{TLS: TLS{
		Store:         map[string][]Certificate{"default": {writeCertFiles(t, "localhost")}},
		ClientCAFiles: []string{trustedCA},
	}}
	cfg, err := h.buildTLSConfig()
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = cfg
	server.StartTLS()
	defer server.Close()

	request := func(certificate *tls.Certificate) error {
		transport := server.Client().Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		if certificate != nil {
			transport.TLSClientConfig.Certificates = []tls.Certificate{*certificate}
		}
		client := &http.Client{Transport: transport}
		res, err := client.Get(server.URL)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}

	now := time.Now()
	trusted := issueTrusted(now.Add(-time.Hour), now.Add(time.Hour))
	unknown := issueUnknown(now.Add(-time.Hour), now.Add(time.Hour))
	expired := issueTrusted(now.Add(-2*time.Hour), now.Add(-time.Hour))

	if err := request(nil); err != nil {
		t.Fatalf("optional client certificate rejected certificate-less request: %v", err)
	}
	if err := request(&trusted); err != nil {
		t.Fatalf("trusted client certificate rejected: %v", err)
	}
	if err := request(&unknown); err == nil {
		t.Fatal("unknown client CA was accepted")
	}
	if err := request(&expired); err == nil {
		t.Fatal("expired client certificate was accepted")
	}
}

func TestTLSMinVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{"", tls.VersionTLS13, false},
		{"1.3", tls.VersionTLS13, false},
		{"1.2", tls.VersionTLS12, false},
		{"1.1", 0, true},
		{"garbage", 0, true},
	}

	for _, tt := range tests {
		got, err := (TLS{MinVersion: tt.in}).minVersion()
		if tt.wantErr {
			if err == nil {
				t.Errorf("minVersion(%q): expected error", tt.in)
			}

			continue
		}

		if err != nil {
			t.Errorf("minVersion(%q): unexpected error %v", tt.in, err)

			continue
		}

		if got != tt.want {
			t.Errorf("minVersion(%q): got %x want %x", tt.in, got, tt.want)
		}
	}
}

func TestWildcardCert(t *testing.T) {
	byHost := map[string]tls.Certificate{
		"*.example.com": {},
	}

	if _, ok := wildcardCert(byHost, "api.example.com"); !ok {
		t.Error("expected wildcard match for api.example.com")
	}

	if _, ok := wildcardCert(byHost, "example.com"); ok {
		t.Error("did not expect wildcard match for bare example.com")
	}

	if _, ok := wildcardCert(byHost, "api.other.com"); ok {
		t.Error("did not expect wildcard match for api.other.com")
	}
}
