package load

import (
	"github.com/hashicorp/consul/api"
	"github.com/worldline-go/igconfig/loader"
)

type Api struct {
	Consul *api.Client
	Vault  loader.Vaulter
}
