package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/cache"
	"github.com/rakunlabs/cache/store/memory"
	storeredis "github.com/rakunlabs/cache/store/redis"
	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	redis "github.com/redis/go-redis/v9"
	"github.com/worldline-go/conn/connredis"
)

var (
	DefaultCodeTimeout  = 10 * time.Second
	DefaultStateTimeout = 2 * time.Minute
)

type Store struct {
	// Active store type empty mean memory or could be redis.
	Active string           `cfg:"active"`
	Redis  connredis.Config `cfg:"redis"`
	// Cluster forces Redis Cluster mode, including when only one bootstrap
	// address is configured. When false, cluster mode is detected automatically.
	Cluster bool `cfg:"cluster"`
	// KeyPrefix namespaces the Redis keys ("<prefix>code_<id>" and
	// "<prefix><state>"). Empty keeps the historic unprefixed keys. A code
	// minted by one component (e.g. login) is only found by another (e.g. the
	// auth token endpoint) when both use the same Redis and the same prefix.
	KeyPrefix string `cfg:"key_prefix"`
}

type StoreCache struct {
	Code  cache.Cacher[string, string]
	State cache.Cacher[string, string]

	redisClient redis.UniversalClient
	keyPrefix   string
	codeTakeM   sync.Mutex
	stateTakeM  sync.Mutex
}

type atomicTaker interface {
	Take(ctx context.Context, key string) (string, bool, error)
}

func (m *Store) Init(ctx context.Context) (*StoreCache, error) {
	var storeCache StoreCache
	if m.Active == "redis" {
		redisClient, err := newRedisClient(ctx, m.Redis, m.Cluster)
		if err != nil {
			return nil, err
		}

		storeCache.redisClient = redisClient
		storeCache.keyPrefix = m.KeyPrefix

		code, err := cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL: DefaultCodeTimeout,
		}))
		if err != nil {
			return nil, err
		}

		state, err := cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL: DefaultStateTimeout,
		}))
		if err != nil {
			return nil, err
		}

		storeCache.Code = prefixed(code, m.KeyPrefix)
		storeCache.State = prefixed(state, m.KeyPrefix)
	} else {
		var err error
		storeCache.Code, err = cache.New(ctx, memory.Store[string, string], cache.WithStoreConfig(&memory.Config{
			TTL: DefaultCodeTimeout,
		}))
		if err != nil {
			return nil, err
		}

		storeCache.State, err = cache.New(ctx, memory.Store[string, string], cache.WithStoreConfig(&memory.Config{
			TTL: DefaultStateTimeout,
		}))
		if err != nil {
			return nil, err
		}
	}

	return &storeCache, nil
}

// newRedisClient supports standalone Redis and Redis Cluster with the same
// configuration. connredis selects cluster mode for multiple seed addresses;
// for a single address we detect whether that server belongs to a cluster.
func newRedisClient(ctx context.Context, cfg connredis.Config, forceCluster bool) (redis.UniversalClient, error) {
	if forceCluster {
		return newRedisClusterClient(cfg)
	}

	client, err := connredis.New(cfg)
	if err != nil {
		return nil, err
	}

	if len(cfg.Address) != 1 {
		return client, nil
	}

	info, err := client.Info(ctx, "cluster").Result()
	if err != nil || !redisClusterEnabled(info) {
		// INFO may be ACL-restricted. Preserve the existing connection and let
		// the first real store operation return any connection/ACL error.
		return client, nil
	}

	clusterClient, err := newRedisClusterClient(cfg)
	if err != nil {
		_ = client.Close()

		return nil, err
	}

	_ = client.Close()

	return clusterClient, nil
}

func newRedisClusterClient(cfg connredis.Config) (redis.UniversalClient, error) {
	if len(cfg.Address) == 0 {
		return nil, errors.New("no address provided")
	}

	tlsConfig, err := cfg.TLS.Generate()
	if err != nil {
		return nil, err
	}

	return redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:      cfg.Address,
		Username:   cfg.UserName,
		Password:   cfg.Password,
		ClientName: cfg.ClientName,
		TLSConfig:  tlsConfig,
	}), nil
}

func redisClusterEnabled(info string) bool {
	for line := range strings.Lines(info) {
		if strings.TrimSpace(line) == "cluster_enabled:1" {
			return true
		}
	}

	return false
}

func (m *StoreCache) Close() error {
	if m.redisClient != nil {
		return m.redisClient.Close()
	}

	return nil
}

func (m *StoreCache) TakeCode(ctx context.Context, key string) (string, bool, error) {
	return m.take(ctx, m.Code, &m.codeTakeM, key)
}

func (m *StoreCache) TakeState(ctx context.Context, key string) (string, bool, error) {
	return m.take(ctx, m.State, &m.stateTakeM, key)
}

func (m *StoreCache) take(ctx context.Context, store cache.Cacher[string, string], lock *sync.Mutex, key string) (string, bool, error) {
	if m.redisClient != nil {
		value, err := m.redisClient.GetDel(ctx, m.keyPrefix+key).Result()
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}

		return value, true, nil
	}

	if taker, ok := store.(atomicTaker); ok {
		return taker.Take(ctx, key)
	}

	lock.Lock()
	defer lock.Unlock()

	value, ok, err := store.Get(ctx, key)
	if err != nil || !ok {
		return "", ok, err
	}
	if err := store.Delete(ctx, key); err != nil {
		return "", false, err
	}

	return value, true, nil
}

// CodeGen stores an authorization code and returns its identifier. The caller
// is expected to fill the client/redirect bindings of code so the token
// endpoint can verify them (RFC 6749 §4.1.3); an unbound code is rejected by
// the auth middleware.
func (m *StoreCache) CodeGen(ctx context.Context, code Code) (string, error) {
	// create code flow response
	codeID, err := oauth2auth.NewState()
	if err != nil {
		return "", err
	}

	codeValue, err := Encode(code)
	if err != nil {
		return "", err
	}

	// save code to store
	if err := m.Code.Set(ctx, "code_"+codeID, codeValue); err != nil {
		return "", err
	}

	return codeID, nil
}

// prefixCache namespaces every key of a cacher.
type prefixCache struct {
	next   cache.Cacher[string, string]
	prefix string
}

func prefixed(next cache.Cacher[string, string], prefix string) cache.Cacher[string, string] {
	if prefix == "" {
		return next
	}

	return &prefixCache{next: next, prefix: prefix}
}

func (c *prefixCache) Get(ctx context.Context, key string) (string, bool, error) {
	return c.next.Get(ctx, c.prefix+key)
}

func (c *prefixCache) Set(ctx context.Context, key, value string) error {
	return c.next.Set(ctx, c.prefix+key, value)
}

func (c *prefixCache) Delete(ctx context.Context, key string) error {
	return c.next.Delete(ctx, c.prefix+key)
}
