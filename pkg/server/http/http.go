package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"
	"github.com/worldline-go/logz/logecho"
	"github.com/worldline-go/turna/pkg/server/cert"
	"github.com/worldline-go/turna/pkg/server/registry"
	"github.com/ziflex/lecho/v3"
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

func (h *HTTP) Set(ctx context.Context, wg *sync.WaitGroup) error {
	if registry.GlobalReg.Echo == nil {
		e := echo.New()

		e.HideBanner = true
		e.Logger = lecho.New(log.With().Str("component", "server").Logger())

		recoverConfig := middleware.DefaultRecoverConfig
		recoverConfig.LogErrorFunc = func(c echo.Context, err error, stack []byte) error {
			log.Error().Err(err).Msgf("panic: %s", stack)

			return err
		}

		// default middlewares
		e.Use(
			middleware.Gzip(),
			middleware.Decompress(),
			middleware.RecoverWithConfig(recoverConfig),
		)

		// log middlewares
		e.Use(
			middleware.RequestID(),
			middleware.RequestLoggerWithConfig(logecho.RequestLoggerConfig()),
			logecho.ZerologLogger(),
		)

		registry.GlobalReg.Echo = e
	}

	for _, listenerName := range registry.GlobalReg.GetListenerNames() {
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
			generated, err := cert.GenerateCertificate()
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
			Handler: registry.GlobalReg.Echo,
			TLSConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: certs,
			},
		}

		listener, err := registry.GlobalReg.GetListener(listenerName)
		if err != nil {
			log.Error().Err(err).Msgf("cannot get listener %s", listenerName)

			continue
		}

		// register server
		registry.GlobalReg.AddHttpServer(listenerName, &s)

		wg.Add(1)
		go func(n string) {
			defer wg.Done()

			log.Info().Msgf("http server %s is listening on %s", n, listener.Addr().String())
			// certificates are loaded from TLSConfig
			if err := s.ServeTLS(listener, "", ""); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msgf("cannot serve listener %s", n)

				registry.GlobalReg.DeleteHttpServer(n)
			}
		}(listenerName)
	}

	for name, middleware := range h.Middlewares {
		if err := middleware.Set(ctx, name); err != nil {
			return err
		}
	}

	for name, router := range h.Routers {
		if err := router.Set(name); err != nil {
			return err
		}
	}

	return nil
}
