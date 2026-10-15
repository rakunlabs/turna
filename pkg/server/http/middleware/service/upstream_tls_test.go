package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newTestCA(t *testing.T) testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ := x509.ParseCertificate(der)

	return testCA{cert: cert, key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

func (ca testCA) server(t *testing.T, dns []string, uris []string) *httptest.Server {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     dns,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, u := range uris {
		parsed, _ := url.Parse(u)
		tmpl.URIs = append(tmpl.URIs, parsed)
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SNI", r.TLS.ServerName)
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}

func TestUpstreamTLS(t *testing.T) {
	ca := newTestCA(t)
	other := newTestCA(t)

	srv := ca.server(t, []string{"abc.example.com"}, []string{"spiffe://abc.example.com/test-identity"})

	for _, tt := range []struct {
		name string
		tls  UpstreamTLS
		ok   bool
	}{
		{"hostname", UpstreamTLS{CA: ca.pem, ServerName: "abc.example.com"}, true},
		{"hostname mismatch", UpstreamTLS{CA: ca.pem, ServerName: "mismatch.example.com"}, false},
		{"other ca", UpstreamTLS{CA: other.pem, ServerName: "abc.example.com"}, false},
		{"system roots", UpstreamTLS{ServerName: "abc.example.com"}, false},
		{"dns san", UpstreamTLS{CA: ca.pem, ServerName: "x.example.com", SubjectAltNames: []string{"abc.example.com"}}, true},
		{"wildcard san", UpstreamTLS{CA: ca.pem, ServerName: "x.example.com", SubjectAltNames: []string{"*.example.com"}}, true},
		{"dns san mismatch", UpstreamTLS{CA: ca.pem, ServerName: "abc.example.com", SubjectAltNames: []string{"other.example.com"}}, false},
		{"uri san", UpstreamTLS{CA: ca.pem, ServerName: "x.example.com", SubjectAltNames: []string{"spiffe://abc.example.com/test-identity"}}, true},
		{"uri san mismatch", UpstreamTLS{CA: ca.pem, ServerName: "abc.example.com", SubjectAltNames: []string{"spiffe://abc.example.com/other"}}, false},
		{"invalid", UpstreamTLS{CA: ca.pem, ServerName: "abc.example.com", Invalid: true}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := tt.tls.config()
			if err != nil {
				t.Fatal(err)
			}

			client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}

			resp, err := client.Get(srv.URL)
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok %v", err, tt.ok)
			}

			if err == nil {
				_ = resp.Body.Close()

				if sni := resp.Header.Get("X-SNI"); sni != tt.tls.ServerName {
					t.Fatalf("sni = %q, want %q", sni, tt.tls.ServerName)
				}
			}
		})
	}

	if _, err := (&UpstreamTLS{CA: "not a certificate"}).config(); err == nil {
		t.Fatal("ca without certificates accepted")
	}
}

// issue returns a PEM client certificate and key signed by ca.
func (ca testCA) issue(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// The client certificate is presented to upstreams requiring one.
func TestUpstreamTLSClientCertificate(t *testing.T) {
	ca := newTestCA(t)
	clients := newTestCA(t)

	srv := ca.server(t, []string{"abc.example.com"}, nil)
	pool := x509.NewCertPool()
	pool.AddCert(clients.cert)
	srv.TLS.ClientAuth = tls.RequireAndVerifyClientCert
	srv.TLS.ClientCAs = pool

	cert, key := clients.issue(t)
	other, otherKey := newTestCA(t).issue(t)

	for _, tt := range []struct {
		name      string
		cert, key string
		ok        bool
	}{
		{"trusted", cert, key, true},
		{"none", "", "", false},
		{"untrusted", other, otherKey, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := (&UpstreamTLS{CA: ca.pem, ServerName: "abc.example.com", ClientCert: tt.cert, ClientKey: tt.key}).config()
			if err != nil {
				t.Fatal(err)
			}

			resp, err := (&http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}).Get(srv.URL)
			if err == nil {
				_ = resp.Body.Close()
			}

			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok %v", err, tt.ok)
			}
		})
	}

	if _, err := (&UpstreamTLS{CA: ca.pem, ClientCert: cert, ClientKey: otherKey}).config(); err == nil {
		t.Fatal("mismatched client key accepted")
	}
}
