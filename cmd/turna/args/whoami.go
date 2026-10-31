package args

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/whoami"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Run a whoami server",
	Long:  "Run a server that answers with host and request details, like traefik/whoami",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runWhoami(cmd.Context())
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

var whoamiCmdFlags = struct {
	address string
	name    string
	env     bool
	cert    string
	key     string
	maxWait time.Duration
}{}

func init() {
	whoamiCmd.Flags().StringVarP(&whoamiCmdFlags.address, "address", "a", ":80", "listen address")
	whoamiCmd.Flags().StringVarP(&whoamiCmdFlags.name, "name", "n", "", "name printed in responses")
	whoamiCmd.Flags().BoolVar(&whoamiCmdFlags.env, "env", false, "allow printing the environment with ?env=true")
	whoamiCmd.Flags().StringVar(&whoamiCmdFlags.cert, "cert", "", "TLS certificate file")
	whoamiCmd.Flags().StringVar(&whoamiCmdFlags.key, "key", "", "TLS key file")
	whoamiCmd.Flags().DurationVar(&whoamiCmdFlags.maxWait, "max-wait", 30*time.Second, "maximum ?wait duration")
}

func runWhoami(ctx context.Context) error {
	if (whoamiCmdFlags.cert == "") != (whoamiCmdFlags.key == "") {
		return errors.New("cert and key must be set together")
	}

	mw, err := (&whoami.Whoami{
		Name:    whoamiCmdFlags.name,
		Env:     whoamiCmdFlags.env,
		MaxWait: whoamiCmdFlags.maxWait,
	}).Middleware()
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              whoamiCmdFlags.address,
		Handler:           mw(http.NotFoundHandler()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("whoami server listening", "address", whoamiCmdFlags.address)

	if whoamiCmdFlags.cert != "" {
		err = server.ListenAndServeTLS(whoamiCmdFlags.cert, whoamiCmdFlags.key)
	} else {
		err = server.ListenAndServe()
	}

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
