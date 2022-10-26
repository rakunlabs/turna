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
	loader := []Loader{}

	if s.Consul != nil {
		loader = append(loader, s.Consul)
	}
	if s.Vault != nil {
		loader = append(loader, s.Vault)
	}
	if s.File != nil {
		loader = append(loader, s.File)
	}

	sort.SliceStable(loader, func(i, j int) bool {
		return loader[i].GetOrder() < loader[j].GetOrder()
	})

	return loader
}

func (s *Static) Load(ctx context.Context, to interface{}) error {
	loader := []Loader{}
	if s.Consul != nil {
		loader = append(loader, s.Consul)
	}
	if s.Vault != nil {
		loader = append(loader, s.Vault)
	}
	if s.File != nil {
		loader = append(loader, s.File)
	}

	sort.Slice(loader, func(i, j int) bool {
		return loader[i].GetOrder() < loader[j].GetOrder()
	})

	for _, l := range loader {
		l.SetDefaults()
		if err := l.Load(ctx, to); err != nil {
			return err
		}
	}

	return nil
}

// Get consul client
func (s *Static) ConsulGetClient() *api.Client {
	return s.Consul.Client
}

// Set consul client
func (s *Static) ConsulSetClient(client *api.Client) {
	s.Consul.Client = client
}

// Get vault client
func (s *Static) VaultGetClient() loader.Vaulter {
	return s.Vault.Client
}

// Set vault client
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

	for _, s := range s {
		if s.Consul != nil {
			s.ConsulSetClient(save.Consul)
		}

		if s.Vault != nil {
			s.VaultSetClient(save.Vault)
		}

		if err := s.Load(ctx, to); err != nil {
			return err
		}

		if s.Consul != nil {
			save.Consul = s.ConsulGetClient()
		}

		if s.Vault != nil {
			save.Vault = s.VaultGetClient()
		}
	}

	return nil
}
