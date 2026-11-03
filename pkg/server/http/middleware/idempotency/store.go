package idempotency

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/rakunlabs/turna/pkg/utils/redisauth"
	"github.com/redis/go-redis/v9"
)

type store interface {
	// setNX stores value when key does not exist and reports whether it did.
	setNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	get(ctx context.Context, key string) ([]byte, bool, error)
	// replace stores value only while key still holds old, so a request
	// whose lock expired cannot overwrite the lock of another one.
	replace(ctx context.Context, key string, old, value []byte, ttl time.Duration) error
	// release removes key only while it still holds old.
	release(ctx context.Context, key string, old []byte) error
}

type memoryItem struct {
	key     string
	value   []byte
	expires time.Time
}

// memoryStore is an LRU store bounded by the size of its values.
type memoryStore struct {
	mu      sync.Mutex
	maxSize int64
	size    int64
	items   map[string]*list.Element
	lru     list.List
	now     func() time.Time
}

func newMemoryStore(maxSize int64) *memoryStore {
	return &memoryStore{maxSize: maxSize, items: make(map[string]*list.Element), now: time.Now}
}

func (s *memoryStore) lookup(key string) *list.Element {
	e, ok := s.items[key]
	if !ok {
		return nil
	}

	if !s.now().Before(e.Value.(*memoryItem).expires) {
		s.remove(e)

		return nil
	}

	return e
}

func (s *memoryStore) setNX(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lookup(key) != nil {
		return false, nil
	}

	s.put(key, value, ttl)

	return true, nil
}

func (s *memoryStore) get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.lookup(key)
	if e == nil {
		return nil, false, nil
	}

	s.lru.MoveToFront(e)

	return e.Value.(*memoryItem).value, true, nil
}

func (s *memoryStore) holds(key string, old []byte) *list.Element {
	e := s.lookup(key)
	if e == nil || !bytes.Equal(e.Value.(*memoryItem).value, old) {
		return nil
	}

	return e
}

func (s *memoryStore) replace(_ context.Context, key string, old, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.holds(key, old) != nil {
		s.put(key, value, ttl)
	}

	return nil
}

func (s *memoryStore) release(_ context.Context, key string, old []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e := s.holds(key, old); e != nil {
		s.remove(e)
	}

	return nil
}

func (s *memoryStore) put(key string, value []byte, ttl time.Duration) {
	if e, ok := s.items[key]; ok {
		s.remove(e)
	}

	size := int64(len(value) + len(key))
	if size > s.maxSize {
		return
	}

	s.items[key] = s.lru.PushFront(&memoryItem{key: key, value: value, expires: s.now().Add(ttl)})
	s.size += size

	for s.size > s.maxSize {
		s.remove(s.lru.Back())
	}
}

func (s *memoryStore) remove(e *list.Element) {
	item := e.Value.(*memoryItem)
	s.lru.Remove(e)
	delete(s.items, item.key)
	s.size -= int64(len(item.value) + len(item.key))
}

type Redis struct {
	storeredis.ConnConfig `cfg:",squash"`
	// GCPIAM authenticates with a Google Cloud access token (Memorystore IAM).
	GCPIAM bool `cfg:"gcp_iam"`

	// KeyPrefix of the keys, default is "turna_idempotency_".
	KeyPrefix string `cfg:"key_prefix"`
}

type redisStore struct {
	client redis.UniversalClient
	prefix string
}

var (
	redisReplace = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
end
return false
`)
	redisRelease = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('DEL', KEYS[1])
end
return 0
`)
)

func newRedisStore(ctx context.Context, cfg *Redis) (*redisStore, error) {
	conn := cfg.ConnConfig
	if err := redisauth.ApplyGCPIAM(&conn, cfg.GCPIAM); err != nil {
		return nil, fmt.Errorf("idempotency redis: %w", err)
	}

	client, err := storeredis.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("idempotency redis: %w", err)
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "turna_idempotency_"
	}

	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	return &redisStore{client: client, prefix: prefix}, nil
}

func (s *redisStore) setNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, s.prefix+key, value, ttl).Result()
}

func (s *redisStore) get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	return v, true, nil
}

func (s *redisStore) replace(ctx context.Context, key string, old, value []byte, ttl time.Duration) error {
	err := redisReplace.Run(ctx, s.client, []string{s.prefix + key}, old, value, ttl.Milliseconds()).Err()
	if errors.Is(err, redis.Nil) {
		return nil
	}

	return err
}

func (s *redisStore) release(ctx context.Context, key string, old []byte) error {
	return redisRelease.Run(ctx, s.client, []string{s.prefix + key}, old).Err()
}
