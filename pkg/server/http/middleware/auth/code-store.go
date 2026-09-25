package auth

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	oauth2store "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/worldline-go/conn/connredis"
)

// CodeStoreSettings configures the temporary OAuth2 code/state cache.
// The same shape is read from the "cache" setting namespace (json) and from
// the static config auth.cache.code_store (cfg).
type CodeStoreSettings struct {
	// Active is "database", "memory" or "redis". Empty defaults to database.
	Active string                 `json:"active" cfg:"active"`
	Redis  CodeStoreRedisSettings `json:"redis"  cfg:"redis"`
	// KeyPrefix namespaces the Redis keys. Empty keeps the unprefixed keys;
	// a login middleware sharing this Redis must use the same prefix.
	KeyPrefix string `json:"key_prefix" cfg:"key_prefix"`
}

type CodeStoreRedisSettings struct {
	ClientName string                    `json:"client_name" cfg:"client_name"`
	Address    []string                  `json:"address"     cfg:"address"`
	Username   string                    `json:"username"    cfg:"username"`
	Password   string                    `json:"password"    cfg:"password" log:"-"`
	TLS        CodeStoreRedisTLSSettings `json:"tls"         cfg:"tls"`
}

type CodeStoreRedisTLSSettings struct {
	Enabled  bool   `json:"enabled"   cfg:"enabled"`
	CertFile string `json:"cert_file" cfg:"cert_file"`
	KeyFile  string `json:"key_file"  cfg:"key_file"`
	CAFile   string `json:"ca_file"   cfg:"ca_file"`
}

// CacheStatic holds instance-local cache overrides from the static config.
type CacheStatic struct {
	// CodeStore pins this instance's OAuth code/state store. When Active is
	// set, the whole code_store of the "cache" setting namespace is ignored on
	// this instance and the UI shows it as read-only. Other instances without
	// an override keep using the stored value.
	CodeStore CodeStoreSettings `cfg:"code_store"`
}

// codeStorePinned reports whether the static config owns the code store.
func (m *Auth) codeStorePinned() bool {
	return strings.TrimSpace(m.Cache.CodeStore.Active) != ""
}

// codeStoreSettings returns the code store settings in effect on this
// instance: the static override when pinned, otherwise the stored setting.
func (m *Auth) codeStoreSettings() CodeStoreSettings {
	if m.codeStorePinned() {
		return m.Cache.CodeStore.normalized()
	}

	return m.cache.Snapshot().Cache.CodeStore.normalized()
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
	store := oauth2store.Store{Active: c.Active, KeyPrefix: c.KeyPrefix}
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
	cfg := m.codeStoreSettings()
	if err := validateCodeStoreSettings(cfg); err != nil {
		if m.codeStorePinned() {
			return nil, fmt.Errorf("static config cache.code_store: %w", err)
		}

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

// IssueAuthorizationCode stores an authorization code in this instance's
// code store, whichever backend is active, and returns its identifier. A
// login middleware in the same process uses it so the code lands exactly
// where this middleware's token endpoint looks for it; writing to a separate
// login store only works when both happen to share one Redis.
func (m *Auth) IssueAuthorizationCode(ctx context.Context, code oauth2store.Code) (string, error) {
	codeStore, err := m.codeStoreRuntime(ctx)
	if err != nil {
		return "", err
	}

	return codeStore.CodeGen(ctx, code)
}
