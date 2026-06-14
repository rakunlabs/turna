package http

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"slices"
	"testing"

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
