package kubernetes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

func (p *Provider) serveHealth(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-p.ready:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	srv := &http.Server{Addr: p.cfg.HealthAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = srv.Shutdown(shutdown)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("kubernetes provider: health server", "err", err.Error())
	}
}
