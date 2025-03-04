package store

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	redis "github.com/redis/go-redis/v9"
	"github.com/worldline-go/cache"
	"github.com/worldline-go/cache/store/memory"
	storeredis "github.com/worldline-go/cache/store/redis"
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
}

type StoreCache struct {
	Code  cache.Cacher[string, string]
	State cache.Cacher[string, string]

	redisClient redis.UniversalClient
}

func (m *Store) Init(ctx context.Context) (*StoreCache, error) {
	var storeCache StoreCache
	if m.Active == "redis" {
		redisClient, err := connredis.New(m.Redis)
		if err != nil {
			return nil, err
		}

		storeCache.redisClient = redisClient

		storeCache.Code, err = cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL: DefaultCodeTimeout,
		}))
		if err != nil {
			return nil, err
		}

		storeCache.State, err = cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL: DefaultStateTimeout,
		}))
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		storeCache.Code, err = cache.New[string, string](ctx, memory.Store, cache.WithStoreConfig(memory.Config{
			TTL: DefaultCodeTimeout,
		}))
		if err != nil {
			return nil, err
		}

		storeCache.State, err = cache.New[string, string](ctx, memory.Store, cache.WithStoreConfig(memory.Config{
			TTL: DefaultStateTimeout,
		}))
		if err != nil {
			return nil, err
		}
	}

	return &storeCache, nil
}

func (m *StoreCache) Close() error {
	if m.redisClient != nil {
		return m.redisClient.Close()
	}

	return nil
}

func (m *StoreCache) CodeGen(ctx context.Context, alias string, scope []string) (string, error) {
	// create code flow response
	codeID := ulid.Make().String()

	codeValue, err := Encode(Code{
		Alias: alias,
		Scope: scope,
	})
	if err != nil {
		return "", err
	}

	// save code to store
	if err := m.Code.Set(ctx, "code_"+codeID, codeValue); err != nil {
		return "", err
	}

	return codeID, nil
}
