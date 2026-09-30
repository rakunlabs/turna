package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
	"github.com/rakunlabs/chu/loader/external/loadervault"
)

// vaultClients caches chu vault loaders by approle login path so each login
// happens once per Load call.
type vaultClients struct {
	loaders map[string]*loadervault.Loader
}

func (c *vaultClients) get(ctx context.Context, appRoleBasePath string) (*loadervault.Loader, error) {
	if l, ok := c.loaders[appRoleBasePath]; ok {
		return l, nil
	}

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	l := &loadervault.Loader{AppRoleBasePath: appRoleBasePath}
	l.SetClient(client)

	// Login uses approle when VAULT_ROLE_ID is set, otherwise the client keeps
	// the token from VAULT_TOKEN.
	if err := l.Login(ctx); err != nil {
		return nil, err
	}

	if c.loaders == nil {
		c.loaders = map[string]*loadervault.Loader{}
	}

	c.loaders[appRoleBasePath] = l

	return l, nil
}

func (c *ConfigVault) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	mountPath := strings.Trim(envOr(c.PathPrefix, "VAULT_SECRET_BASE_PATH"), "/")
	if mountPath == "" {
		return nil, "", errors.New("vault: path_prefix is required, set path_prefix or VAULT_SECRET_BASE_PATH env")
	}

	key := strings.Trim(c.Path, "/")
	if key == "" {
		return nil, "", errors.New("vault: path is required")
	}

	if _, ok := os.LookupEnv("VAULT_ADDR"); !ok {
		if _, ok := os.LookupEnv("VAULT_AGENT_ADDR"); !ok {
			return nil, "", errors.New("vault: VAULT_ADDR or VAULT_AGENT_ADDR env is required")
		}
	}

	l, err := cl.vault.get(ctx, c.AppRoleBasePath)
	if err != nil {
		return nil, "", fmt.Errorf("vault: %w", err)
	}

	vMap, err := l.LoadKey(ctx, mountPath, key)
	if err != nil {
		return nil, "", fmt.Errorf("vault %s/%s: %w", mountPath, key, err)
	}

	// KV data is a map; hand it over as JSON so the shared processing
	// (template, raw, inner_path, map, base64) applies like other sources.
	data, err := json.Marshal(vMap)
	if err != nil {
		return nil, "", fmt.Errorf("vault %s/%s: %w", mountPath, key, err)
	}

	return data, "JSON", nil
}
