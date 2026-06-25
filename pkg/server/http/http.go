package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server/cert"
	"github.com/rakunlabs/turna/pkg/server/registry"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

type HTTP struct {
	Routers     map[string]Router         `cfg:"routers"`
	Middlewares map[string]HTTPMiddleware `cfg:"middlewares"`
	TLS         TLS                       `cfg:"tls"`
}

type TLS struct {
	// Store maps an SNI host name to its certificate(s). The special key
	// "default" is used as the fallback when the client sends no SNI server
	// name or no host entry matches.
	Store map[string][]Certificate `cfg:"store"`
	// MinVersion is the minimum accepted TLS version: "1.2" or "1.3".
	// Defaults to "1.3".
	MinVersion string `cfg:"min_version"`
	// SelfSigned customizes the auto-generated certificate used when no
	// certificate is configured in Store.
	SelfSigned SelfSigned `cfg:"self_signed"`
	// ACME enables automatic certificate provisioning from an ACME CA such as
	// Let's Encrypt using the TLS-ALPN-01 challenge.
	ACME *ACME `cfg:"acme"`
}

// ACME configures automatic certificate provisioning from an ACME CA
// (e.g. Let's Encrypt) using the TLS-ALPN-01 challenge over the existing TLS
// entrypoint. No extra HTTP port is required, but the TLS entrypoint (usually
// :443) must be reachable from the public internet for validation to succeed.
type ACME struct {
	// Enabled turns on ACME certificate provisioning.
	Enabled bool `cfg:"enabled"`
	// Email is the contact address registered with the ACME account.
	Email string `cfg:"email"`
	// Domains is the allow-list of host names ACME certificates may be issued
	// for (HostWhitelist). A request for a host outside this list is rejected.
	Domains []string `cfg:"domains"`
	// CacheDir is the directory used to persist account keys and issued
	// certificates. Defaults to "acme-cache".
	CacheDir string `cfg:"cache_dir"`
	// DirectoryURL overrides the ACME directory endpoint. Leave empty for the
	// Let's Encrypt production CA. Use the staging URL while testing to avoid
	// rate limits: https://acme-staging-v02.api.letsencrypt.org/directory
	DirectoryURL string `cfg:"directory_url"`
}

type SelfSigned struct {
	Organization []string `cfg:"organization"`
	DNSNames     []string `cfg:"dns_names"`
	IPs          []string `cfg:"ips"`
}

type Certificate struct {
	CertFile string `cfg:"cert_file"`
	KeyFile  string `cfg:"key_file"`
}

// minVersion converts the configured MinVersion string to a tls constant.
// Default is TLS 1.3.
func (t TLS) minVersion() (uint16, error) {
	switch t.MinVersion {
	case "", "1.3", "13", "tls1.3":
		return tls.VersionTLS13, nil
	case "1.2", "12", "tls1.2":
		return tls.VersionTLS12, nil
	default:
		return 0, fmt.Errorf("unsupported tls min_version %q (use \"1.2\" or \"1.3\")", t.MinVersion)
	}
}

// buildTLSConfig builds an SNI-aware *tls.Config from the configured Store.
// Every Store host key is loaded, and GetCertificate selects a certificate by
// the client's SNI server name (exact, then wildcard), falling back to the
// "default" host key and finally to a generated self-signed certificate.
func (h *HTTP) buildTLSConfig() (*tls.Config, error) {
	minVersion, err := h.TLS.minVersion()
	if err != nil {
		return nil, err
	}

	byHost := make(map[string]tls.Certificate, len(h.TLS.Store))
	allCerts := make([]tls.Certificate, 0, len(h.TLS.Store))

	for host, certList := range h.TLS.Store {
		for _, c := range certList {
			certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("cannot load certificate for host %q: %w, certFile: %s, keyFile: %s", host, err, c.CertFile, c.KeyFile)
			}

			// first certificate wins per host key
			if _, ok := byHost[host]; !ok {
				byHost[host] = certificate
			}
			allCerts = append(allCerts, certificate)
		}
	}

	// Determine the fallback certificate.
	var fallback tls.Certificate
	switch {
	case len(byHost["default"].Certificate) > 0:
		fallback = byHost["default"]
	case len(allCerts) > 0:
		fallback = allCerts[0]
	default:
		generated, err := h.generateSelfSigned()
		if err != nil {
			return nil, err
		}
		fallback = generated
		allCerts = append(allCerts, generated)
	}

	// Optionally build an ACME (e.g. Let's Encrypt) certificate manager that
	// provisions certificates on demand via the TLS-ALPN-01 challenge.
	var acmeManager *autocert.Manager
	if h.TLS.ACME != nil && h.TLS.ACME.Enabled {
		acmeManager = h.buildACMEManager()
	}

	cfg := &tls.Config{
		MinVersion:   minVersion,
		Certificates: allCerts,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// A TLS-ALPN-01 challenge handshake must always be answered by the
			// ACME manager, even when a static certificate is configured for the
			// same host, otherwise validation fails.
			if acmeManager != nil && isACMEChallenge(hello) {
				return acmeManager.GetCertificate(hello)
			}

			if hello.ServerName != "" {
				if certificate, ok := byHost[hello.ServerName]; ok {
					return &certificate, nil
				}
				if certificate, ok := wildcardCert(byHost, hello.ServerName); ok {
					return certificate, nil
				}
			}

			// No statically configured certificate matched; let ACME provision
			// a certificate for the requested host.
			if acmeManager != nil {
				certificate, err := acmeManager.GetCertificate(hello)
				if err == nil {
					return certificate, nil
				}
				slog.Debug("acme could not provide certificate, using fallback",
					"server_name", hello.ServerName, "err", err.Error())
			}

			return &fallback, nil
		},
	}

	if acmeManager != nil {
		// Advertise the ACME TLS-ALPN-01 protocol while preserving normal
		// HTTP/2 and HTTP/1.1 negotiation for regular traffic.
		cfg.NextProtos = append(cfg.NextProtos, "h2", "http/1.1", acme.ALPNProto)
	}

	return cfg, nil
}

// isACMEChallenge reports whether the ClientHello negotiates the ACME
// TLS-ALPN-01 challenge protocol ("acme-tls/1").
func isACMEChallenge(hello *tls.ClientHelloInfo) bool {
	for _, proto := range hello.SupportedProtos {
		if proto == acme.ALPNProto {
			return true
		}
	}

	return false
}

// buildACMEManager constructs an autocert.Manager from the ACME config. The
// manager handles account registration, certificate issuance and automatic
// renewal, persisting state under the configured cache directory.
func (h *HTTP) buildACMEManager() *autocert.Manager {
	cacheDir := h.TLS.ACME.CacheDir
	if cacheDir == "" {
		cacheDir = "acme-cache"
	}

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cacheDir),
		Email:  h.TLS.ACME.Email,
	}

	if len(h.TLS.ACME.Domains) > 0 {
		manager.HostPolicy = autocert.HostWhitelist(h.TLS.ACME.Domains...)
	}

	if h.TLS.ACME.DirectoryURL != "" {
		manager.Client = &acme.Client{DirectoryURL: h.TLS.ACME.DirectoryURL}
	}

	return manager
}

// wildcardCert looks up a certificate for serverName by replacing its first
// label with "*" (e.g. "api.example.com" -> "*.example.com").
func wildcardCert(byHost map[string]tls.Certificate, serverName string) (*tls.Certificate, bool) {
	if i := strings.IndexByte(serverName, '.'); i > 0 {
		if certificate, ok := byHost["*"+serverName[i:]]; ok {
			return &certificate, true
		}
	}

	return nil, false
}

// generateSelfSigned builds a cached self-signed certificate, honoring the
// optional SelfSigned config.
func (h *HTTP) generateSelfSigned() (tls.Certificate, error) {
	opts := make([]cert.Options, 0, 3)
	if len(h.TLS.SelfSigned.Organization) > 0 {
		opts = append(opts, cert.WithOrganization(h.TLS.SelfSigned.Organization...))
	}
	if len(h.TLS.SelfSigned.DNSNames) > 0 {
		opts = append(opts, cert.WithDNSNames(h.TLS.SelfSigned.DNSNames...))
	}
	if len(h.TLS.SelfSigned.IPs) > 0 {
		opts = append(opts, cert.WithIPs(h.TLS.SelfSigned.IPs...))
	}

	generated, err := cert.GenerateCertificateCache(opts...)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot generate self-signed certificate: %w", err)
	}

	certificate, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot load generated certificate: %w", err)
	}

	return certificate, nil
}

var ReadHeaderTimeout = 10 * time.Second

func (h *HTTP) Set(ctx context.Context, wg *sync.WaitGroup) error {
	ruleRouter := NewRuleRouter()
	// check routers entrypoints
	allEntries := registry.GlobalReg.GetListenerNames()
	selectedEntries := make(map[string]struct{})
	selectedEntriesTLS := make(map[string]struct{})
	for _, router := range h.Routers {
		for _, entrypoint := range router.EntryPoints {
			if _, ok := allEntries[entrypoint]; !ok {
				return fmt.Errorf("entrypoint %s does not exist", entrypoint)
			}
		}

		entrypoints := router.EntryPoints
		if len(entrypoints) == 0 {
			for entrypoint := range allEntries {
				entrypoints = append(entrypoints, entrypoint)
			}
		}

		tlsEnabled := router.TLS != nil

		for _, entrypoint := range entrypoints {
			if tlsEnabled {
				selectedEntriesTLS[entrypoint] = struct{}{}
			} else {
				selectedEntries[entrypoint] = struct{}{}
			}

			ruleRouter.SetRule(RuleSelection{Host: router.Host, Entrypoint: entrypoint})
		}
	}

	// build a shared, SNI-aware TLS config once when any TLS entrypoint exists
	var tlsConfig *tls.Config
	if len(selectedEntriesTLS) > 0 {
		c, err := h.buildTLSConfig()
		if err != nil {
			return err
		}

		tlsConfig = c
	}

	// entrypoints for TLS
	for entrypoint := range selectedEntriesTLS {
		s := http.Server{
			ReadHeaderTimeout: ReadHeaderTimeout,
			Handler:           ruleRouter.Serve(entrypoint),
			TLSConfig:         tlsConfig.Clone(),
		}

		listener, err := registry.GlobalReg.GetListener(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get listener %s", entrypoint), "err", err.Error())

			continue
		}

		// register server
		registry.GlobalReg.AddHttpServer(entrypoint+"-TLS", &s)

		wg.Add(1)
		go func(n string) {
			defer func() {
				slog.Info(fmt.Sprintf("http tls-server [%s] is stopped", n))
				registry.GlobalReg.DeleteHttpServer(n + "-TLS")

				wg.Done()
			}()

			slog.Info(fmt.Sprintf("http tls-server [%s] is listening on %s", n, listener.Addr().String()))
			// certificates are loaded from TLSConfig
			if err := s.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error(fmt.Sprintf("cannot serve tls listener [%s]", n), "err", err.Error())
			}
		}(entrypoint)
	}

	// entrypoints without TLS
	for entrypoint := range selectedEntries {
		s := http.Server{
			ReadHeaderTimeout: ReadHeaderTimeout,
			Handler:           ruleRouter.Serve(entrypoint),
		}

		listener, err := registry.GlobalReg.GetListener(entrypoint)
		if err != nil {
			slog.Error(fmt.Sprintf("cannot get listener [%s]", entrypoint), "err", err.Error())

			continue
		}

		// register server
		registry.GlobalReg.AddHttpServer(entrypoint, &s)

		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			defer slog.Info(fmt.Sprintf("http server [%s] is stopped", n))

			slog.Info(fmt.Sprintf("http server [%s] is listening on %s", n, listener.Addr().String()))
			// certificates are loaded from TLSConfig
			if err := s.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error(fmt.Sprintf("cannot serve listener [%s]", n), "err", err.Error())

				registry.GlobalReg.DeleteHttpServer(n)
			}
		}(entrypoint)
	}

	for name, middleware := range h.Middlewares {
		if err := middleware.Set(ctx, name); err != nil {
			return fmt.Errorf("middleware %s cannot set: %w", name, err)
		}
	}

	// init middlewares
	if err := registry.GlobalReg.RunHTTPInitFuncs(); err != nil {
		return fmt.Errorf("cannot init http middlewares: %w", err)
	}

	for name, router := range h.Routers {
		if err := router.Set(name, ruleRouter); err != nil {
			return fmt.Errorf("router %s cannot set: %w", name, err)
		}
	}

	return nil
}
