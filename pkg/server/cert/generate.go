package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"
)

type Certificate struct {
	Certificate []byte
	PrivateKey  []byte
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*Certificate{}
)

// GenerateCertificateCache returns a certificate generated once per set of
// options.
func GenerateCertificateCache(opts ...Options) (*Certificate, error) {
	o := newOptions(opts...)
	key := fmt.Sprintf("%q|%q|%v|%d", o.Organization, o.DNSNames, o.IPs, o.NotAfter)

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if c, ok := cache[key]; ok {
		return c, nil
	}

	c, err := generate(o)
	if err != nil {
		return nil, err
	}

	cache[key] = c

	return c, nil
}

func newOptions(opts ...Options) *options {
	o := &options{
		Organization: []string{"turna"},
		DNSNames:     []string{"localhost"},
		IPs:          []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotAfter:     365 * 24 * time.Hour,
	}
	for _, opt := range opts {
		opt(o)
	}

	return o
}

func GenerateCertificate(opts ...Options) (*Certificate, error) {
	return generate(newOptions(opts...))
}

func generate(o *options) (*Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)

	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: o.Organization,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(o.NotAfter),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              o.DNSNames,
		IPAddresses:           o.IPs,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	privBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	privateKeyByte := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if privateKeyByte == nil {
		return nil, fmt.Errorf("failed to encode private key")
	}

	certificateByte := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if certificateByte == nil {
		return nil, fmt.Errorf("failed to encode certificate")
	}

	return &Certificate{
		Certificate: certificateByte,
		PrivateKey:  privateKeyByte,
	}, nil
}
