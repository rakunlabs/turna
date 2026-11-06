package acme

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/redis/go-redis/v9"

	"github.com/rakunlabs/turna/pkg/utils/redisauth"
)

// Redis stores certificates, account keys and locks in Redis, so several
// Turna instances share certificates and coordinate issuance and renewals.
type Redis struct {
	storeredis.ConnConfig `cfg:",squash"`
	// GCPIAM authenticates with a Google Cloud access token (Memorystore IAM).
	GCPIAM bool `cfg:"gcp_iam"`

	// KeyPrefix of the Redis keys, default is "turna_acme_".
	KeyPrefix string `cfg:"key_prefix"`
}

const (
	redisLockTTL     = time.Minute
	redisLockRefresh = 20 * time.Second
	redisLockPoll    = time.Second
)

// unlockScript deletes the lock only when it is still owned by the token.
var unlockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('DEL', KEYS[1])
end
return 0
`)

// refreshScript extends the lock only when it is still owned by the token.
var refreshScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
	return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`)

// redisStorage implements certmagic.Storage. Values and modification times
// are kept in two hashes sharing a hash tag, so it also works on Redis
// Cluster and listing does not need SCAN.
type redisStorage struct {
	client redis.UniversalClient

	dataKey string
	metaKey string
	lockKey string

	mu    sync.Mutex
	locks map[string]*redisLock
}

type redisLock struct {
	token string
	stop  chan struct{}
}

var _ certmagic.Storage = (*redisStorage)(nil)

func newRedisStorage(ctx context.Context, cfg *Redis) (*redisStorage, error) {
	conn := cfg.ConnConfig
	if err := redisauth.ApplyGCPIAM(&conn, cfg.GCPIAM); err != nil {
		return nil, fmt.Errorf("acme redis: %w", err)
	}

	client, err := storeredis.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("acme redis: %w", err)
	}

	return newRedisStorageClient(client, cfg.KeyPrefix), nil
}

func newRedisStorageClient(client redis.UniversalClient, prefix string) *redisStorage {
	if prefix == "" {
		prefix = "turna_acme_"
	}

	tag := "{" + prefix + "}"

	return &redisStorage{
		client:  client,
		dataKey: tag + "data",
		metaKey: tag + "meta",
		lockKey: tag + "lock:",
		locks:   make(map[string]*redisLock),
	}
}

func (s *redisStorage) Close() error {
	s.mu.Lock()
	for name, l := range s.locks {
		close(l.stop)
		delete(s.locks, name)
	}
	s.mu.Unlock()

	return s.client.Close()
}

func (s *redisStorage) Store(ctx context.Context, key string, value []byte) error {
	_, err := s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, s.dataKey, key, value)
		p.HSet(ctx, s.metaKey, key, strconv.FormatInt(time.Now().UnixNano(), 10))

		return nil
	})

	return err
}

func (s *redisStorage) Load(ctx context.Context, key string) ([]byte, error) {
	v, err := s.client.HGet(ctx, s.dataKey, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, fs.ErrNotExist
	}

	return v, err
}

// children returns the stored keys below dir.
func (s *redisStorage) children(ctx context.Context, dir string) ([]string, error) {
	keys, err := s.client.HKeys(ctx, s.dataKey).Result()
	if err != nil {
		return nil, err
	}

	prefix := ""
	if dir = strings.Trim(dir, "/"); dir != "" {
		prefix = dir + "/"
	}

	out := make([]string, 0)
	for _, k := range keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}

	return out, nil
}

func (s *redisStorage) Delete(ctx context.Context, key string) error {
	fields, err := s.children(ctx, key)
	if err != nil {
		return err
	}

	fields = append(fields, key)

	_, err = s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HDel(ctx, s.dataKey, fields...)
		p.HDel(ctx, s.metaKey, fields...)

		return nil
	})

	return err
}

func (s *redisStorage) Exists(ctx context.Context, key string) bool {
	if ok, err := s.client.HExists(ctx, s.dataKey, key).Result(); err == nil && ok {
		return true
	}

	children, err := s.children(ctx, key)

	return err == nil && len(children) > 0
}

func (s *redisStorage) List(ctx context.Context, dir string, recursive bool) ([]string, error) {
	children, err := s.children(ctx, dir)
	if err != nil {
		return nil, err
	}

	if len(children) == 0 {
		return nil, fs.ErrNotExist
	}

	dir = strings.Trim(dir, "/")
	seen := make(map[string]struct{})

	for _, k := range children {
		rel := strings.TrimPrefix(k, dir)
		rel = strings.TrimPrefix(rel, "/")
		parts := strings.Split(rel, "/")

		if !recursive {
			seen[path.Join(dir, parts[0])] = struct{}{}

			continue
		}

		for i := range parts {
			seen[path.Join(dir, path.Join(parts[:i+1]...))] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}

	slices.Sort(out)

	return out, nil
}

func (s *redisStorage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {
	size, err := s.client.HStrLen(ctx, s.dataKey, key).Result()
	if err != nil {
		return certmagic.KeyInfo{}, err
	}

	if size > 0 {
		info := certmagic.KeyInfo{Key: key, Size: size, IsTerminal: true}
		if v, err := s.client.HGet(ctx, s.metaKey, key).Int64(); err == nil {
			info.Modified = time.Unix(0, v)
		}

		return info, nil
	}

	// an empty value is stored too
	if ok, err := s.client.HExists(ctx, s.dataKey, key).Result(); err == nil && ok {
		return certmagic.KeyInfo{Key: key, IsTerminal: true}, nil
	}

	children, err := s.children(ctx, key)
	if err != nil {
		return certmagic.KeyInfo{}, err
	}

	if len(children) == 0 {
		return certmagic.KeyInfo{}, fs.ErrNotExist
	}

	// a directory is as new as its newest key, used to find the latest
	// account
	info := certmagic.KeyInfo{Key: key, IsTerminal: false}

	mods, err := s.client.HMGet(ctx, s.metaKey, children...).Result()
	if err != nil {
		return certmagic.KeyInfo{}, err
	}

	for _, v := range mods {
		str, _ := v.(string)
		if n, err := strconv.ParseInt(str, 10, 64); err == nil {
			if t := time.Unix(0, n); t.After(info.Modified) {
				info.Modified = t
			}
		}
	}

	return info, nil
}

// Lock blocks until the named lock is acquired. The lock expires on its own
// when the holder disappears and is refreshed while held.
func (s *redisStorage) Lock(ctx context.Context, name string) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}

	token := hex.EncodeToString(b)
	key := s.lockKey + name

	for {
		ok, err := s.client.SetNX(ctx, key, token, redisLockTTL).Result()
		if err != nil {
			return fmt.Errorf("acme redis lock %s: %w", name, err)
		}

		if ok {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(redisLockPoll):
		}
	}

	l := &redisLock{token: token, stop: make(chan struct{})}

	s.mu.Lock()
	if old, ok := s.locks[name]; ok {
		close(old.stop)
	}
	s.locks[name] = l
	s.mu.Unlock()

	go s.refresh(key, l)

	return nil
}

func (s *redisStorage) refresh(key string, l *redisLock) {
	ticker := time.NewTicker(redisLockRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = refreshScript.Run(ctx, s.client, []string{key}, l.token, redisLockTTL.Milliseconds()).Err()
			cancel()
		}
	}
}

func (s *redisStorage) Unlock(ctx context.Context, name string) error {
	s.mu.Lock()
	l, ok := s.locks[name]
	if ok {
		close(l.stop)
		delete(s.locks, name)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("acme redis lock %s is not held", name)
	}

	return unlockScript.Run(ctx, s.client, []string{s.lockKey + name}, l.token).Err()
}

func (s *redisStorage) String() string {
	return "redis:" + s.dataKey
}
