package args

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/worldline-go/klient"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Trigger api",
	Long:  "Trigger api with optional parameters",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAPI(cmd.Context())
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

var apiCmdFlags = struct {
	// ping deprecated, use slient instead
	ping       bool
	slient     bool
	url        []string
	skipVerify bool
	method     string
}{}

func init() {
	apiCmd.Flags().BoolVar(&apiCmdFlags.ping, "ping", false, "check is healthy")
	apiCmd.Flags().BoolVarP(&apiCmdFlags.slient, "slient", "s", false, "slient mode")
	apiCmd.Flags().StringArrayVarP(&apiCmdFlags.url, "url", "u", nil, "url to call ex: http://localhost:8080/ping")
	apiCmd.Flags().BoolVarP(&apiCmdFlags.skipVerify, "insecure", "k", false, "skip verify")
	apiCmd.Flags().StringVarP(&apiCmdFlags.method, "method", "m", "GET", "http method")
}

func runAPI(ctx context.Context) error {
	client, err := klient.New(
		klient.WithPooledClient(false),
		klient.WithInsecureSkipVerify(apiCmdFlags.skipVerify),
		klient.WithDisableRetry(true),
		klient.WithDisableBaseURLCheck(true),
		klient.WithLogger(slog.Default()),
	)
	if err != nil {
		return err
	}

	for _, url := range apiCmdFlags.url {
		req, err := http.NewRequestWithContext(ctx, apiCmdFlags.method, url, nil)
		if err != nil {
			return err
		}

		if err := client.Do(req, func(r *http.Response) error {
			if !(r.StatusCode >= 200 && r.StatusCode < 300) {
				if !apiCmdFlags.slient {
					w := bufio.NewWriter(os.Stderr)
					_, _ = bufio.NewReader(r.Body).WriteTo(w)

					w.Flush()
				}

				return fmt.Errorf("unexpected status code: %d", r.StatusCode)
			}

			if !apiCmdFlags.slient {
				w := bufio.NewWriter(os.Stdout)
				_, _ = bufio.NewReader(r.Body).WriteTo(w)

				w.Flush()
			}

			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}
