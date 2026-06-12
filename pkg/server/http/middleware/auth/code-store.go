package auth

import (
	"context"
	"reflect"
	"strings"

	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/worldline-go/conn/connredis"
)

// CodeStoreSettings configures the temporary OAuth2 code/state cache.
type CodeStoreSettings struct {
	// Active is "memory" or "redis". Empty keeps the in-process memory store.
	Active string                 `json:"active"`
	Redis  CodeStoreRedisSettings `json:"redis"`
}

type CodeStoreRedisSettings struct {
	ClientName string                    `json:"client_name"`
	Address    []string                  `json:"address"`
	Username   string                    `json:"username"`
	Password   string                    `json:"password"`
	TLS        CodeStoreRedisTLSSettings `json:"tls"`
}

type CodeStoreRedisTLSSettings struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
}

func (c CodeStoreSettings) normalized() CodeStoreSettings {
	c.Active = strings.ToLower(strings.TrimSpace(c.Active))
	if c.Active == "" || c.Active != "redis" {
		c.Active = "memory"
	}

	return c
}

func (c CodeStoreSettings) store() oauth2store.Store {
	c = c.normalized()
	store := oauth2store.Store{Active: c.Active}
	if c.Active != "redis" {
		return store
	}

	store.Redis = connredis.Config{
		ClientName: c.Redis.ClientName,
		Address:    c.Redis.Address,
		UserName:   c.Redis.Username,
		Password:   c.Redis.Password,
		TLS: connredis.TLSConfig{
			Enabled:  c.Redis.TLS.Enabled,
			CertFile: c.Redis.TLS.CertFile,
			KeyFile:  c.Redis.TLS.KeyFile,
			CAFile:   c.Redis.TLS.CAFile,
		},
	}

	return store
}

func (m *Auth) codeStoreRuntime(ctx context.Context) (*oauth2store.StoreCache, error) {
	cfg := m.cache.Snapshot().Cache.CodeStore.normalized()

	m.codeStoreM.Lock()
	defer m.codeStoreM.Unlock()

	if m.codeStore != nil && reflect.DeepEqual(m.codeStoreCfg, cfg) {
		return m.codeStore, nil
	}

	storeConfig := cfg.store()
	storeCache, err := storeConfig.Init(ctx)
	if err != nil {
		return nil, err
	}

	oldStore := m.codeStore
	m.codeStore = storeCache
	m.codeStoreCfg = cfg
	if oldStore != nil {
		_ = oldStore.Close()
	}

	return storeCache, nil
}

func (m *Auth) closeCodeStore() error {
	m.codeStoreM.Lock()
	defer m.codeStoreM.Unlock()

	if m.codeStore == nil {
		return nil
	}

	return m.codeStore.Close()
}
