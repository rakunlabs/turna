package args

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/worldline-go/utility/httpx"
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
	ping       bool
	url        []string
	skipVerify bool
	method     string
}{}

func init() {
	apiCmd.Flags().BoolVar(&apiCmdFlags.ping, "ping", false, "check is healthy")
	apiCmd.Flags().StringArrayVarP(&apiCmdFlags.url, "url", "u", nil, "url to call ex: http://localhost:8080/ping")
	apiCmd.Flags().BoolVarP(&apiCmdFlags.skipVerify, "insecure", "k", false, "skip verify")
	apiCmd.Flags().StringVarP(&apiCmdFlags.method, "method", "m", "GET", "http method")
}

func runAPI(ctx context.Context) error {
	if apiCmdFlags.ping {
		return ping(ctx)
	}

	return nil
}

func ping(ctx context.Context) error {
	call, err := httpx.NewClient(
		httpx.WithZerologLogger(log.Logger),
		httpx.WithPooledClient(false),
		httpx.WithInsecureSkipVerify(apiCmdFlags.skipVerify),
		httpx.WithDisableRetry(true),
		httpx.WithDisableBaseURLCheck(true),
	)

	if err != nil {
		return err
	}

	for _, url := range apiCmdFlags.url {
		err := call.RequestWithURL(ctx,
			apiCmdFlags.method,
			url,
			nil, nil,
			func(r *http.Response) error {
				if !(r.StatusCode >= 200 && r.StatusCode < 300) {
					w := bufio.NewWriter(os.Stderr)
					_, _ = bufio.NewReader(r.Body).WriteTo(w)

					w.Flush()

					return fmt.Errorf("unexpected status code: %d", r.StatusCode)
				}

				w := bufio.NewWriter(os.Stdout)
				_, _ = bufio.NewReader(r.Body).WriteTo(w)

				w.Flush()

				return nil
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}
