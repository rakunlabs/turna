package static

import (
	"context"
	"fmt"

	"github.com/hashicorp/consul/api"
	"github.com/worldline-go/igconfig/loader"
)

type Consul struct {
	// set nil to client to refresh connection
	Client     *api.Client `cfg:"-"`
	Path       string      `cfg:"path"`
	PathPrefix string      `cfg:"path_prefix"`
	Order      int         `cfg:"order"`
}

func (l Consul) GetOrder() int {
	return l.Order
}

func (l *Consul) SetDefaults() {}

func (l Consul) Load(ctx context.Context, to interface{}) error {
	loader.ConsulConfigPathPrefix = l.PathPrefix

	c := loader.Consul{Client: l.Client}
	defer func() {
		l.Client = c.Client
	}()

	if err := c.LoadWithContext(ctx, l.Path, to); err != nil {
		return fmt.Errorf("failed consul load; %w", err)
	}

	return nil
}
