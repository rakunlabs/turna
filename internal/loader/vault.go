package loader

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/vault/api"
)

// vaultClient is a small wrapper over the vault KVv2 API used by loads.
type vaultClient struct {
	client          *api.Client
	appRoleBasePath string
}

func (c *vaultClient) connect() error {
	if c.client != nil {
		return nil
	}

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return fmt.Errorf("failed to create vault client: %w", err)
	}

	c.client = client

	return c.login(context.Background())
}

func (c *vaultClient) login(ctx context.Context) error {
	// A combination of a Role ID and Secret ID is required to log in with an
	// AppRole. The role ID is provided by the Vault administrator.
	roleID := os.Getenv("VAULT_ROLE_ID")
	if roleID == "" {
		return fmt.Errorf("no role ID was provided in VAULT_ROLE_ID env var")
	}

	appRoleBasePath := c.appRoleBasePath
	if appRoleBasePath == "" {
		appRoleBasePath = os.Getenv("VAULT_APPROLE_BASE_PATH")
	}

	if appRoleBasePath == "" {
		appRoleBasePath = "auth/approle/login"
	}

	secret, err := c.client.Logical().WriteWithContext(ctx, appRoleBasePath, map[string]interface{}{
		"role_id":   roleID,
		"secret_id": os.Getenv("VAULT_ROLE_SECRET"),
	})
	if err != nil {
		return fmt.Errorf("failed to login to vault: %w", err)
	}

	c.client.SetToken(secret.Auth.ClientToken)

	return nil
}

// loadMap loads a KVv2 secret as a map.
func (c *vaultClient) loadMap(ctx context.Context, mountPath, key string) (map[string]interface{}, error) {
	if err := c.connect(); err != nil {
		return nil, err
	}

	secret, err := c.client.KVv2(mountPath).Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return secret.Data, nil
}
