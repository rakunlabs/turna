package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/worldline-go/turna/pkg/server/cert"
	"github.com/worldline-go/turna/pkg/server/registry"
)

type HTTP struct {
	Routers     map[string]Router         `cfg:"routers"`
	Middlewares map[string]HTTPMiddleware `cfg:"middlewares"`
	TLS         TLS                       `cfg:"tls"`
}

type TLS struct {
	Store map[string][]Certificate `cfg:"store"`
}

type Certificate struct {
	CertFile string `cfg:"cert_file"`
	KeyFile  string `cfg:"key_file"`
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

	// entrypoints for TLS
	for entrypoint := range selectedEntriesTLS {
		certs := []tls.Certificate{}
		// get default keypair
		if defaultCert, ok := h.TLS.Store["default"]; ok {
			for _, cert := range defaultCert {
				certificate, err := tls.LoadX509KeyPair(cert.CertFile, cert.KeyFile)
				if err != nil {
					return fmt.Errorf("cannot load default certificate: %w, certFile: %s, keyFile: %s", err, cert.CertFile, cert.KeyFile)
				}

				certs = append(certs, certificate)
			}
		}

		if len(certs) == 0 {
			// generate and add self-signed certificate
			generated, err := cert.GenerateCertificateCache()
			if err != nil {
				return fmt.Errorf("cannot generate self-signed certificate: %w", err)
			}

			certificate, err := tls.X509KeyPair(generated.Certificate, generated.PrivateKey)
			if err != nil {
				return fmt.Errorf("cannot load generated certificate: %w", err)
			}

			certs = append(certs, certificate)
		}

		s := http.Server{
			ReadHeaderTimeout: ReadHeaderTimeout,
			Handler:           ruleRouter.Serve(entrypoint),
			TLSConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: certs,
			},
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
			if err := s.ServeTLS(listener, "", ""); err != nil && errors.Is(err, http.ErrServerClosed) {
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
			if err := s.Serve(listener); err != nil && errors.Is(err, http.ErrServerClosed) {
				slog.Error(fmt.Sprintf("cannot serve listener [%s]", n), "err", err.Error())

				registry.GlobalReg.DeleteHttpServer(n)
			}
		}(entrypoint)
	}

	for name, middleware := range h.Middlewares {
		if err := middleware.Set(ctx, name); err != nil {
			return err
		}
	}

	// init middlewares
	if err := registry.GlobalReg.RunHTTPInitFuncs(); err != nil {
		return err
	}

	for name, router := range h.Routers {
		if err := router.Set(name, ruleRouter); err != nil {
			return err
		}
	}

	return nil
}
