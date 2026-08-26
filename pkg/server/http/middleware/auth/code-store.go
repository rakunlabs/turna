package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/worldline-go/conn/connredis"
)

// CodeStoreSettings configures the temporary OAuth2 code/state cache.
type CodeStoreSettings struct {
	// Active is "database", "memory" or "redis". Empty defaults to database.
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
	if c.Active == "" {
		c.Active = "database"
	}

	return c
}

func validateCodeStoreSettings(c CodeStoreSettings) error {
	switch c.normalized().Active {
	case "database", "memory", "redis":
		return nil
	default:
		return errors.New("code_store.active must be database, memory or redis")
	}
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

type databaseCodeCache struct {
	store *Store
	kind  string
	ttl   time.Duration
}

func (c *databaseCodeCache) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	if err := c.store.GetFlowCode(ctx, c.kind, key, &value); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return "", false, nil
		}

		return "", false, err
	}

	return value, true, nil
}

func (c *databaseCodeCache) Set(ctx context.Context, key, value string) error {
	return c.store.PutFlowCode(ctx, c.kind, key, value, c.ttl)
}

func (c *databaseCodeCache) Delete(ctx context.Context, key string) error {
	return c.store.DeleteFlowCode(ctx, c.kind, key)
}

func (c *databaseCodeCache) Take(ctx context.Context, key string) (string, bool, error) {
	var value string
	if err := c.store.TakeFlowCode(ctx, c.kind, key, &value); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return "", false, nil
		}

		return "", false, err
	}

	return value, true, nil
}

func (m *Auth) codeStoreRuntime(ctx context.Context) (*oauth2store.StoreCache, error) {
	cfg := m.cache.Snapshot().Cache.CodeStore.normalized()
	if err := validateCodeStoreSettings(cfg); err != nil {
		return nil, err
	}

	m.codeStoreM.Lock()
	defer m.codeStoreM.Unlock()

	if m.codeStore != nil && reflect.DeepEqual(m.codeStoreCfg, cfg) {
		return m.codeStore, nil
	}

	var storeCache *oauth2store.StoreCache
	if cfg.Active == "database" {
		if m.store == nil {
			return nil, errors.New("database code store is unavailable")
		}

		storeCache = &oauth2store.StoreCache{
			Code:  &databaseCodeCache{store: m.store, kind: flowKindOAuthCode, ttl: oauth2store.DefaultCodeTimeout},
			State: &databaseCodeCache{store: m.store, kind: flowKindOAuthState, ttl: oauth2store.DefaultStateTimeout},
		}
	} else {
		storeConfig := cfg.store()
		var err error
		storeCache, err = storeConfig.Init(ctx)
		if err != nil {
			return nil, err
		}
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
