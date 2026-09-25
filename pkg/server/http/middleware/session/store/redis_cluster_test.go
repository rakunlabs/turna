package store

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedisClusterCanBeForcedWithOneAddress(t *testing.T) {
	client, err := (Redis{Address: "redis:6379", Cluster: true}).newClient(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if _, ok := client.(*redis.ClusterClient); !ok {
		t.Fatalf("client = %T, want *redis.ClusterClient", client)
	}
}

func TestRedisClusterEnabled(t *testing.T) {
	tests := []struct {
		name string
		info string
		want bool
	}{
		{name: "cluster", info: "# Cluster\r\ncluster_enabled:1\r\n", want: true},
		{name: "standalone", info: "# Cluster\r\ncluster_enabled:0\r\n"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redisClusterEnabled(tt.info); got != tt.want {
				t.Fatalf("redisClusterEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
