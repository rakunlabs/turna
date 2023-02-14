package args

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/rytsh/liz/loader/httpx"
	"github.com/spf13/cobra"
	"github.com/worldline-go/logz"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Trigger api",
	Long:  "Trigger api with optional parameters",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAPI(cmd.Context())
	},
	SilenceUsage: true,
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
	call := httpx.New(
		httpx.WithLog(logz.AdapterKV{Log: log.Logger}),
		httpx.WithPooled(false),
		httpx.WithSkipVerify(apiCmdFlags.skipVerify),
	)

	for _, url := range apiCmdFlags.url {
		response, err := call.Send(ctx,
			url,
			apiCmdFlags.method,
			nil, nil,
			&httpx.Retry{DisableRetry: true},
			apiCmdFlags.skipVerify)
		if err != nil {
			return err
		}

		if !(response.StatusCode >= 200 && response.StatusCode < 300) {
			os.Stderr.Write(response.Body)
			return fmt.Errorf("unexpected status code: %d", response.StatusCode)
		}

		os.Stdout.Write(response.Body)
	}

	return nil
}
