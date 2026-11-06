// Package acme provisions and renews certificates from an ACME CA such as
// Let's Encrypt or ZeroSSL with certmagic. It supports the HTTP-01,
// TLS-ALPN-01 and DNS-01 challenges, on-demand issuance, OCSP stapling and
// shared storage between instances.
package acme

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/mholt/acmez/v3"
	acmeapi "github.com/mholt/acmez/v3/acme"

	"github.com/rakunlabs/turna/pkg/utils/zapslog"
)

// ACME configures automatic certificates.
type ACME struct {
	// Enabled turns on ACME certificate provisioning.
	Enabled bool `cfg:"enabled"`
	// Email is the contact address registered with the ACME account.
	Email string `cfg:"email"`
	// Domains are obtained at startup and renewed in the background.
	// Wildcards such as *.example.com need the dns challenge.
	Domains []string `cfg:"domains"`

	// CA is letsencrypt (default), letsencrypt_staging, zerossl or a
	// directory URL.
	CA string `cfg:"ca"`
	// DirectoryURL overrides CA with an ACME directory endpoint.
	DirectoryURL string `cfg:"directory_url"`
	// EAB is the external account binding some CAs (ZeroSSL) require.
	EAB *EAB `cfg:"eab"`
	// KeyType of certificate keys: ed25519, p256 (default), p384, rsa2048,
	// rsa4096.
	KeyType string `cfg:"key_type"`

	// CacheDir is the directory used to persist account keys and
	// certificates when redis is not set. Defaults to "acme-cache".
	CacheDir string `cfg:"cache_dir"`
	// Redis stores certificates and locks in Redis, shared by instances.
	Redis *Redis `cfg:"redis"`

	// DisableHTTPChallenge turns off HTTP-01. The challenge is answered on
	// the plain HTTP entrypoints; port 80 must be reachable.
	DisableHTTPChallenge bool `cfg:"disable_http_challenge"`
	// DisableTLSALPNChallenge turns off TLS-ALPN-01, answered on the TLS
	// entrypoints; port 443 must be reachable.
	DisableTLSALPNChallenge bool `cfg:"disable_tls_alpn_challenge"`
	// DNS enables the DNS-01 challenge. When set, only DNS-01 is used.
	DNS *DNS `cfg:"dns"`

	// OnDemand obtains certificates during the first TLS handshake of a
	// host not in Domains.
	OnDemand *OnDemand `cfg:"on_demand"`

	// DisableOCSPStapling turns off OCSP stapling.
	DisableOCSPStapling bool `cfg:"disable_ocsp_stapling"`
}

type EAB struct {
	KeyID  string `cfg:"key_id"`
	MACKey string `cfg:"mac_key" log:"-"`
}

// OnDemand decides which hosts may get a certificate at handshake time.
// At least one of Ask or Allow is required, otherwise anyone could make
// Turna request certificates for any host pointing to it.
type OnDemand struct {
	// Ask is a URL called with ?domain=<host>; a 2xx answer allows it.
	Ask string `cfg:"ask"`
	// Allow lists host patterns, e.g. *.example.com, that are allowed
	// without asking.
	Allow []string `cfg:"allow"`
	// AskTimeout default 10s.
	AskTimeout time.Duration `cfg:"ask_timeout"`
}

const (
	caLetsEncrypt        = "letsencrypt"
	caLetsEncryptStaging = "letsencrypt_staging"
	caZeroSSL            = "zerossl"
)

// Fingerprint identifies the configuration, an unchanged configuration
// keeps its running manager on reload.
func (a *ACME) Fingerprint() string {
	v, err := json.Marshal(a)
	if err != nil {
		return ""
	}

	return string(v)
}

func (a *ACME) directory() (string, error) {
	if a.DirectoryURL != "" {
		return a.DirectoryURL, nil
	}

	switch strings.ToLower(a.CA) {
	case "", caLetsEncrypt:
		return certmagic.LetsEncryptProductionCA, nil
	case caLetsEncryptStaging:
		return certmagic.LetsEncryptStagingCA, nil
	case caZeroSSL:
		return certmagic.ZeroSSLProductionCA, nil
	}

	if u, err := url.Parse(a.CA); err == nil && (u.Scheme == "https" || u.Scheme == "http") {
		return a.CA, nil
	}

	return "", fmt.Errorf("unknown ca %q, use %s, %s, %s or a directory url", a.CA, caLetsEncrypt, caLetsEncryptStaging, caZeroSSL)
}

func keyType(v string) (certmagic.KeyType, error) {
	switch kt := certmagic.KeyType(strings.ToLower(v)); kt {
	case "":
		return certmagic.P256, nil
	case certmagic.ED25519, certmagic.P256, certmagic.P384, certmagic.RSA2048, certmagic.RSA4096, certmagic.RSA8192:
		return kt, nil
	}

	return "", fmt.Errorf("unsupported key_type %q", v)
}

func normalize(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		if d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), "."); d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}

	return out
}

// Manager serves and maintains ACME certificates.
type Manager struct {
	config      *certmagic.Config
	cache       *certmagic.Cache
	issuer      *certmagic.ACMEIssuer
	storage     certmagic.Storage
	fingerprint string
	domains     []string
	tlsALPN     bool

	ctx    context.Context
	cancel context.CancelFunc
}

// New validates the configuration and builds a manager without obtaining
// certificates; Start begins the management and Stop releases it.
func New(ctx context.Context, a *ACME) (*Manager, error) {
	m, err := newManager(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("acme: %w", err)
	}

	return m, nil
}

func newManager(ctx context.Context, a *ACME) (*Manager, error) {
	directory, err := a.directory()
	if err != nil {
		return nil, err
	}

	kt, err := keyType(a.KeyType)
	if err != nil {
		return nil, err
	}

	domains := normalize(a.Domains)
	for _, d := range domains {
		if strings.HasPrefix(d, "*.") && a.DNS == nil {
			return nil, fmt.Errorf("wildcard domain %q needs the dns challenge", d)
		}

		if strings.Contains(strings.TrimPrefix(d, "*."), "*") {
			return nil, fmt.Errorf("domain %q has an unsupported wildcard", d)
		}
	}

	if a.OnDemand == nil && len(domains) == 0 {
		return nil, errors.New("domains or on_demand is required")
	}

	if a.DNS == nil && a.DisableHTTPChallenge && a.DisableTLSALPNChallenge {
		return nil, errors.New("all challenges are disabled")
	}

	var solver *certmagic.DNS01Solver
	if a.DNS != nil {
		if solver, err = a.DNS.solver(); err != nil {
			return nil, err
		}
	}

	var decision func(ctx context.Context, name string) error
	if a.OnDemand != nil {
		if decision, err = a.OnDemand.decision(domains); err != nil {
			return nil, err
		}
	}

	var eab *acmeapi.EAB
	if a.EAB != nil {
		if a.EAB.KeyID == "" || a.EAB.MACKey == "" {
			return nil, errors.New("eab needs key_id and mac_key")
		}

		eab = &acmeapi.EAB{KeyID: a.EAB.KeyID, MACKey: a.EAB.MACKey}
	}

	mctx, cancel := context.WithCancel(ctx)

	var storage certmagic.Storage
	if a.Redis != nil {
		rs, err := newRedisStorage(mctx, a.Redis)
		if err != nil {
			cancel()

			return nil, err
		}

		storage = rs
	} else {
		dir := a.CacheDir
		if dir == "" {
			dir = "acme-cache"
		}

		storage = &certmagic.FileStorage{Path: dir}
	}

	logger := zapslog.New(slog.Default().With("component", "acme"))

	m := &Manager{
		storage:     storage,
		fingerprint: a.Fingerprint(),
		domains:     domains,
		tlsALPN:     a.DNS == nil && !a.DisableTLSALPNChallenge,
		cancel:      cancel,
	}

	m.cache = certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return m.config, nil
		},
		Logger: logger,
	})

	m.config = certmagic.New(m.cache, certmagic.Config{
		Storage:   storage,
		KeySource: certmagic.StandardKeyGenerator{KeyType: kt},
		OCSP:      certmagic.OCSPConfig{DisableStapling: a.DisableOCSPStapling},
		Logger:    logger,
	})

	if decision != nil {
		m.config.OnDemand = &certmagic.OnDemandConfig{DecisionFunc: decision}
	}

	m.issuer = certmagic.NewACMEIssuer(m.config, certmagic.ACMEIssuer{
		CA:                      directory,
		TestCA:                  directory,
		Email:                   a.Email,
		Agreed:                  true,
		ExternalAccount:         eab,
		DisableHTTPChallenge:    a.DisableHTTPChallenge,
		DisableTLSALPNChallenge: a.DisableTLSALPNChallenge,
		DNS01Solver:             solver,
		Logger:                  logger,
	})
	m.config.Issuers = []certmagic.Issuer{m.issuer}

	m.ctx = mctx

	return m, nil
}

// Start obtains the certificates of Domains in the background and keeps
// them renewed.
func (m *Manager) Start() error {
	if len(m.domains) == 0 {
		return nil
	}

	if err := m.config.ManageAsync(m.ctx, m.domains); err != nil {
		return fmt.Errorf("acme: %w", err)
	}

	return nil
}

func (o *OnDemand) decision(domains []string) (func(ctx context.Context, name string) error, error) {
	allow := append(normalize(o.Allow), domains...)

	if o.Ask == "" && len(o.Allow) == 0 {
		return nil, errors.New("on_demand needs ask or allow")
	}

	var ask *url.URL
	if o.Ask != "" {
		u, err := url.Parse(o.Ask)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("on_demand ask %q must be an http(s) url", o.Ask)
		}

		ask = u
	}

	timeout := o.AskTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{Timeout: timeout}

	return func(ctx context.Context, name string) error {
		name = strings.ToLower(name)

		for _, p := range allow {
			if p == name || certmagic.MatchWildcard(name, p) {
				return nil
			}
		}

		if ask == nil {
			return fmt.Errorf("%s is not allowed", name)
		}

		u := *ask
		q := u.Query()
		q.Set("domain", name)
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("on_demand ask: %w", err)
		}

		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("on_demand ask denied %s with status %d", name, resp.StatusCode)
		}

		return nil
	}, nil
}

// Fingerprint of the configuration the manager was built with.
func (m *Manager) Fingerprint() string {
	return m.fingerprint
}

// TLSALPN reports whether the acme-tls/1 protocol must be advertised.
func (m *Manager) TLSALPN() bool {
	return m.tlsALPN
}

// IsChallenge reports whether hello is a TLS-ALPN-01 validation handshake.
func IsChallenge(hello *tls.ClientHelloInfo) bool {
	return len(hello.SupportedProtos) == 1 && hello.SupportedProtos[0] == acmez.ACMETLS1Protocol
}

// GetCertificate returns the managed certificate of the handshake, or an
// error when ACME has none for it.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return m.config.GetCertificate(hello)
}

// HandleHTTPChallenge answers an HTTP-01 validation request and reports
// whether it did. Challenges started by other instances sharing the storage
// are answered too.
func (m *Manager) HandleHTTPChallenge(w http.ResponseWriter, r *http.Request) bool {
	if m == nil || !certmagic.LooksLikeHTTPChallenge(r) {
		return false
	}

	return m.issuer.HandleHTTPChallenge(w, r)
}

// Stop ends certificate maintenance and closes the storage.
func (m *Manager) Stop() {
	m.cancel()
	m.cache.Stop()

	if c, ok := m.storage.(io.Closer); ok {
		_ = c.Close()
	}
}
