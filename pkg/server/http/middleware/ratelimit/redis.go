package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	storeredis "github.com/rakunlabs/cache/store/redis"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	storeredis.ConnConfig `cfg:",squash"`

	// KeyPrefix of the counter keys, default is "turna_ratelimit_".
	KeyPrefix string `cfg:"key_prefix"`
}

// Both counters share a hash tag so the script also runs on Redis Cluster.
var redisScript = redis.NewScript(`
local curr = tonumber(redis.call('GET', KEYS[1]) or '0')
local prev = tonumber(redis.call('GET', KEYS[2]) or '0')
local rate = prev * tonumber(ARGV[2]) + curr
if rate + 1 > tonumber(ARGV[1]) then
	return {0, tostring(rate)}
end
curr = redis.call('INCR', KEYS[1])
if curr == 1 then
	redis.call('PEXPIRE', KEYS[1], ARGV[3])
end
return {1, tostring(rate + 1)}
`)

type redisStore struct {
	slidingWindow

	client redis.UniversalClient
	prefix string
}

func newRedisStore(ctx context.Context, cfg *Redis, limit int, window time.Duration) (*redisStore, error) {
	client, err := storeredis.NewClient(ctx, cfg.ConnConfig)
	if err != nil {
		return nil, fmt.Errorf("rate_limit redis: %w", err)
	}

	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "turna_ratelimit_"
	}

	go func() {
		<-ctx.Done()
		_ = client.Close()
	}()

	return &redisStore{
		slidingWindow: slidingWindow{limit: limit, window: window},
		client:        client,
		prefix:        prefix,
	}, nil
}

func (s *redisStore) allow(ctx context.Context, key string) (result, error) {
	index, elapsed := s.windowAt(time.Now())

	base := s.prefix + "{" + key + "}:"
	keys := []string{base + strconv.FormatInt(index, 10), base + strconv.FormatInt(index-1, 10)}

	// the current counter is still read as previous during the next window
	ttl := 2 * s.window.Milliseconds()

	v, err := redisScript.Run(ctx, s.client, keys, s.limit, s.weight(elapsed), ttl).Slice()
	if err != nil {
		return result{}, err
	}

	if len(v) != 2 {
		return result{}, fmt.Errorf("unexpected redis reply %v", v)
	}

	allowed, _ := v[0].(int64)
	rateStr, _ := v[1].(string)
	rate, _ := strconv.ParseFloat(rateStr, 64)

	return s.result(allowed == 1, rate, elapsed), nil
}
