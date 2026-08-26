package store

import (
	"context"
	"errors"
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
}

type StoreCache struct {
	Code  cache.Cacher[string, string]
	State cache.Cacher[string, string]

	redisClient redis.UniversalClient
	codeTakeM   sync.Mutex
	stateTakeM  sync.Mutex
}

type atomicTaker interface {
	Take(ctx context.Context, key string) (string, bool, error)
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
		value, err := m.redisClient.GetDel(ctx, key).Result()
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

func (m *StoreCache) CodeGen(ctx context.Context, alias string, scope []string) (string, error) {
	// create code flow response
	codeID, err := oauth2auth.NewState()
	if err != nil {
		return "", err
	}

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
