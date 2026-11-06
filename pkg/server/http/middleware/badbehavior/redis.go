package badbehavior

import (
	"context"
	"fmt"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/rakunlabs/turna/pkg/utils/redisauth"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	storeredis.ConnConfig `cfg:",squash"`
	// GCPIAM authenticates with a Google Cloud access token (Memorystore IAM).
	GCPIAM bool `cfg:"gcp_iam"`

	// KeyPrefix of the counter and ban keys, default is "turna_badbehavior_".
	KeyPrefix string `cfg:"key_prefix"`
}

// Both keys share a hash tag so the script also runs on Redis Cluster.
var hitScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[2]) == 1 then
	return 0
end
local count = redis.call('INCR', KEYS[1])
if count == 1 then
	redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
if count < tonumber(ARGV[1]) then
	return 0
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], '1', 'PX', ARGV[3])
return 1
`)

type redisStore struct {
	client    redis.UniversalClient
	prefix    string
	threshold int
	window    time.Duration
	banTime   time.Duration
}

func newRedisStore(ctx context.Context, cfg *Redis, threshold int, window, banTime time.Duration) (*redisStore, error) {
	conn := cfg.ConnConfig
	if err := redisauth.ApplyGCPIAM(&conn, cfg.GCPIAM); err != nil {
		return nil, fmt.Errorf("bad_behavior redis: %w", err)
	}

	client, err := storeredis.NewClient(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("bad_behavior redis: %w", err)
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "turna_badbehavior_"
	}

	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	return &redisStore{
		client:    client,
		prefix:    prefix,
		threshold: threshold,
		window:    window,
		banTime:   banTime,
	}, nil
}

func (s *redisStore) keys(ip string) []string {
	base := s.prefix + "{" + ip + "}:"

	return []string{base + "count", base + "ban"}
}

func (s *redisStore) banned(ctx context.Context, ip string) (time.Duration, error) {
	ttl, err := s.client.PTTL(ctx, s.keys(ip)[1]).Result()
	if err != nil {
		return 0, err
	}

	// -2 missing key, -1 no expiry which is never set by hit
	if ttl < 0 {
		return 0, nil
	}

	return ttl, nil
}

func (s *redisStore) hit(ctx context.Context, ip string) (bool, error) {
	v, err := hitScript.Run(ctx, s.client, s.keys(ip), s.threshold, s.window.Milliseconds(), s.banTime.Milliseconds()).Int()
	if err != nil {
		return false, err
	}

	return v == 1, nil
}
