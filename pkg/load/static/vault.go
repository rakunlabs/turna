package static

import (
	"context"
	"fmt"

	"github.com/worldline-go/igconfig/loader"
)

type Vault struct {
	// set nil to client to refresh connection
	Client          loader.Vaulter          `cfg:"-"`
	Path            string                  `cfg:"path"`
	PathPrefix      string                  `cfg:"path_prefix"`
	AppRoleBasePath string                  `cfg:"app_role_base_path"`
	AdditionalPaths []loader.AdditionalPath `cfg:"additional_paths"`
	Order           int                     `cfg:"order"`
	Map             string                  `cfg:"map"`
}

func (l Vault) GetOrder() int {
	return l.Order
}

func (l *Vault) SetDefaults() {
	if l.AppRoleBasePath == "" {
		l.AppRoleBasePath = "auth/approle/login"
	}
}

func (l Vault) Load(ctx context.Context, to interface{}) error {
	loader.VaultSecretBasePath = l.PathPrefix
	loader.VaultSecretAdditionalPaths = l.AdditionalPaths
	loader.VaultAppRoleBasePath = l.AppRoleBasePath

	c := loader.Vault{}
	defer func() {
		l.Client = c.Client
	}()

	if err := c.LoadWithContext(ctx, l.Path, to); err != nil {
		return fmt.Errorf("failed vault load; %w", err)
	}

	return nil
}
