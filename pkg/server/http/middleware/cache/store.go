package cache

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/redis/go-redis/v9"
)

type store interface {
	get(ctx context.Context, key string) ([]byte, bool, error)
	set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	del(ctx context.Context, key string) error
}

type memoryItem struct {
	key     string
	value   []byte
	expires time.Time
}

// memoryStore is an LRU cache bounded by the size of its values.
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

func (s *memoryStore) get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.items[key]
	if !ok {
		return nil, false, nil
	}

	item := e.Value.(*memoryItem)
	if !s.now().Before(item.expires) {
		s.remove(e)

		return nil, false, nil
	}

	s.lru.MoveToFront(e)

	return item.value, true, nil
}

func (s *memoryStore) set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.items[key]; ok {
		s.remove(e)
	}

	size := int64(len(value) + len(key))
	if size > s.maxSize {
		return nil
	}

	s.items[key] = s.lru.PushFront(&memoryItem{key: key, value: value, expires: s.now().Add(ttl)})
	s.size += size

	for s.size > s.maxSize {
		s.remove(s.lru.Back())
	}

	return nil
}

func (s *memoryStore) del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.items[key]; ok {
		s.remove(e)
	}

	return nil
}

func (s *memoryStore) remove(e *list.Element) {
	item := e.Value.(*memoryItem)
	s.lru.Remove(e)
	delete(s.items, item.key)
	s.size -= int64(len(item.value) + len(item.key))
}

type Redis struct {
	storeredis.ConnConfig `cfg:",squash"`

	// KeyPrefix of the cache keys, default is "turna_cache_".
	KeyPrefix string `cfg:"key_prefix"`
}

type redisStore struct {
	client redis.UniversalClient
	prefix string
}

func newRedisStore(ctx context.Context, cfg *Redis) (*redisStore, error) {
	client, err := storeredis.NewClient(ctx, cfg.ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("cache redis: %w", err)
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "turna_cache_"
	}

	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	return &redisStore{client: client, prefix: prefix}, nil
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

func (s *redisStore) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, s.prefix+key, value, ttl).Err()
}

func (s *redisStore) del(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.prefix+key).Err()
}
