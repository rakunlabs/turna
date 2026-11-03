// Package redisauth configures Redis authentication that needs more than a
// static password.
package redisauth

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/auth/credentials"
	storeredis "github.com/rakunlabs/cache/store/redis"
)

const gcpCloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// ApplyGCPIAM makes cfg authenticate with a Google Cloud access token from
// Application Default Credentials, for Memorystore with IAM authentication.
// It does nothing when enabled is false.
func ApplyGCPIAM(cfg *storeredis.ConnConfig, enabled bool) error {
	if !enabled {
		return nil
	}

	if cfg.Username != "" || cfg.Password != "" {
		return errors.New("gcp_iam cannot be combined with username or password")
	}

	creds, err := credentials.DetectDefault(&credentials.DetectOptions{
		Scopes: []string{gcpCloudPlatformScope},
	})
	if err != nil {
		return fmt.Errorf("gcp_iam credentials: %w", err)
	}

	// The auth library caches and refreshes the token, so it is not cached
	// again here; every new connection asks for the current one.
	cfg.CredentialsProvider = func(ctx context.Context) (string, string, error) {
		token, err := creds.Token(ctx)
		if err != nil {
			return "", "", fmt.Errorf("gcp_iam token: %w", err)
		}

		return "default", token.Value, nil
	}

	return nil
}
