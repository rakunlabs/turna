package ldapview

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCA returns a CA in PEM and a server TLS config signed by it for 127.0.0.1.
func testCA(t *testing.T) (string, *tls.Config) {
	t.Helper()

	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test LDAP CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	ca, _ := x509.ParseCertificate(caDER)

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))

	return caPEM, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
}

func TestLDAPSCustomCA(t *testing.T) {
	t.Parallel()

	caPEM, cfg := testCA(t)
	srv := newFakeLDAPTLS(t, cfg)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte(caPEM), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &LdapView{Servers: []Server{
		{Name: "system", Addr: srv.addr},
		{Name: "inline", Addr: srv.addr, CA: caPEM},
		{Name: "file", Addr: srv.addr, CAFile: caFile},
		{Name: "insecure", Addr: srv.addr, InsecureSkipVerify: true},
	}}

	mw, err := m.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	h := mw(nil)

	for _, tc := range []struct {
		name   string
		req    loginRequest
		status int
		body   string
	}{
		{name: "system pool refuses", req: loginRequest{Server: "system", Mode: BindAnonymous}, status: 401, body: "failed to verify certificate"},
		{name: "inline ca", req: loginRequest{Server: "inline", Mode: BindAnonymous}, status: 200, body: `"custom_ca":true`},
		{name: "ca file", req: loginRequest{Server: "file", Mode: BindAnonymous}, status: 200, body: `"custom_ca":true`},
		{name: "skip verify", req: loginRequest{Server: "insecure", Mode: BindAnonymous}, status: 200, body: `"insecure":true`},
		{name: "custom url with ca", req: loginRequest{Addr: srv.addr, CA: caPEM, Mode: BindAnonymous}, status: 200, body: `"custom_ca":true`},
		{name: "custom url without ca", req: loginRequest{Addr: srv.addr, Mode: BindAnonymous}, status: 401, body: "failed to verify certificate"},
		{name: "custom url bad ca", req: loginRequest{Addr: srv.addr, CA: "not a cert", Mode: BindAnonymous}, status: 400, body: "no PEM certificate"},
	} {
		w := do(t, h, http.MethodPost, "/api/login", tc.req, nil)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body)
		}
	}
}

func TestCAConfigErrors(t *testing.T) {
	t.Parallel()

	for _, s := range []Server{
		{Addr: "ldaps://x", CAFile: "/does/not/exist.pem"},
		{Addr: "ldaps://x", CA: "garbage"},
	} {
		if _, err := (&LdapView{Servers: []Server{s}}).Middleware(); err == nil {
			t.Errorf("want error for %+v", s)
		}
	}
}
