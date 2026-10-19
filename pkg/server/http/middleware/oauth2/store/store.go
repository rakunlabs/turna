package store

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/rakunlabs/cache"
	"github.com/rakunlabs/cache/store/memory"
	storeredis "github.com/rakunlabs/cache/store/redis"
	oauth2auth "github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	redis "github.com/redis/go-redis/v9"
)

var (
	DefaultCodeTimeout  = 10 * time.Second
	DefaultStateTimeout = 2 * time.Minute
)

type Store struct {
	// Active store type empty mean memory or could be redis.
	Active string                `cfg:"active"`
	Redis  storeredis.ConnConfig `cfg:"redis"`
	// Cluster forces Redis Cluster mode, including when only one bootstrap
	// address is configured. When false, cluster mode is detected automatically.
	// Same as redis.cluster.
	Cluster bool `cfg:"cluster"`
	// KeyPrefix namespaces the Redis keys ("<prefix>code_<id>" and
	// "<prefix><state>"). Empty keeps the historic unprefixed keys. A code
	// minted by one component (e.g. login) is only found by another (e.g. the
	// auth token endpoint) when both use the same Redis and the same prefix.
	KeyPrefix string `cfg:"key_prefix"`
}

// StoreCache holds the code and state caches. Both must implement
// cache.Taker so that codes and states are consumed exactly once.
type StoreCache struct {
	Code  cache.Cacher[string, string]
	State cache.Cacher[string, string]

	redisClient redis.UniversalClient
}

func (m *Store) Init(ctx context.Context) (*StoreCache, error) {
	if m.Active == "redis" {
		cfg := m.Redis
		cfg.Cluster = cfg.Cluster || m.Cluster

		redisClient, err := storeredis.NewClient(ctx, cfg)
		if err != nil {
			return nil, err
		}

		code, err := cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL:       DefaultCodeTimeout,
			KeyPrefix: m.KeyPrefix,
		}))
		if err != nil {
			_ = redisClient.Close()

			return nil, err
		}

		state, err := cache.New(ctx, storeredis.Store(redisClient), cache.WithStoreConfig(storeredis.Config{
			TTL:       DefaultStateTimeout,
			KeyPrefix: m.KeyPrefix,
		}))
		if err != nil {
			_ = redisClient.Close()

			return nil, err
		}

		return &StoreCache{Code: code, State: state, redisClient: redisClient}, nil
	}

	code, err := cache.New(ctx, memory.Store[string, string], cache.WithStoreConfig(&memory.Config{
		TTL: DefaultCodeTimeout,
	}))
	if err != nil {
		return nil, err
	}

	state, err := cache.New(ctx, memory.Store[string, string], cache.WithStoreConfig(&memory.Config{
		TTL: DefaultStateTimeout,
	}))
	if err != nil {
		_ = code.Close()

		return nil, err
	}

	return &StoreCache{Code: code, State: state}, nil
}

func (m *StoreCache) Close() error {
	var errs []error
	for _, c := range []cache.Cacher[string, string]{m.Code, m.State} {
		if closer, ok := c.(io.Closer); ok {
			errs = append(errs, closer.Close())
		}
	}
	if m.redisClient != nil {
		errs = append(errs, m.redisClient.Close())
	}

	return errors.Join(errs...)
}

func (m *StoreCache) TakeCode(ctx context.Context, key string) (string, bool, error) {
	return take(ctx, m.Code, key)
}

func (m *StoreCache) TakeState(ctx context.Context, key string) (string, bool, error) {
	return take(ctx, m.State, key)
}

// take consumes key atomically. There is deliberately no Get+Delete fallback:
// it would let two instances sharing a store redeem the same code.
func take(ctx context.Context, store cache.Cacher[string, string], key string) (string, bool, error) {
	if taker, ok := store.(cache.Taker[string, string]); ok {
		return taker.Take(ctx, key)
	}

	return "", false, cache.ErrTakeNotSupported
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
