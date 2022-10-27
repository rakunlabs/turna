package load

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/consul/api"
	"github.com/worldline-go/igconfig/loader"
	"github.com/worldline-go/turna/pkg/load/static"
)

type Static struct {
	Consul *static.Consul
	Vault  *static.Vault
	File   *static.File
}

type Loader interface {
	SetDefaults()
	Load(ctx context.Context, to interface{}) error
	GetOrder() int
}

func (s *Static) GetLoaders() []Loader {
	load := []Loader{}

	if s.Consul != nil {
		load = append(load, s.Consul)
	}
	if s.Vault != nil {
		load = append(load, s.Vault)
	}
	if s.File != nil {
		load = append(load, s.File)
	}

	sort.SliceStable(load, func(i, j int) bool {
		return load[i].GetOrder() < load[j].GetOrder()
	})

	return load
}

func (s *Static) Load(ctx context.Context, to interface{}) error {
	load := []Loader{}
	if s.Consul != nil {
		load = append(load, s.Consul)
	}
	if s.Vault != nil {
		load = append(load, s.Vault)
	}
	if s.File != nil {
		load = append(load, s.File)
	}

	sort.Slice(load, func(i, j int) bool {
		return load[i].GetOrder() < load[j].GetOrder()
	})

	for _, l := range load {
		l.SetDefaults()
		if err := l.Load(ctx, to); err != nil {
			return err
		}
	}

	return nil
}

// Get consul client.
func (s *Static) ConsulGetClient() *api.Client {
	return s.Consul.Client
}

// Set consul client.
func (s *Static) ConsulSetClient(client *api.Client) {
	s.Consul.Client = client
}

// Get vault client.
func (s *Static) VaultGetClient() loader.Vaulter {
	return s.Vault.Client
}

// Set vault client.
func (s *Static) VaultSetClient(client loader.Vaulter) {
	s.Vault.Client = client
}

type Statics []Static

func (s Statics) Load(ctx context.Context, to interface{}, save *Api) error {
	if to == nil {
		return fmt.Errorf("to is nil")
	}

	if save == nil {
		save = &Api{}
	}

	for _, static := range s {
		if static.Consul != nil {
			static.ConsulSetClient(save.Consul)
		}

		if static.Vault != nil {
			static.VaultSetClient(save.Vault)
		}

		if err := static.Load(ctx, to); err != nil {
			return err
		}

		if static.Consul != nil {
			save.Consul = static.ConsulGetClient()
		}

		if static.Vault != nil {
			save.Vault = static.VaultGetClient()
		}
	}

	return nil
}
