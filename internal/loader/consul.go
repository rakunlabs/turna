package loader

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	hclog "github.com/hashicorp/go-hclog"
)

// consulClient is a small wrapper over the consul KV API used by loads.
type consulClient struct {
	client *api.Client
	kv     *api.KV

	queryOptions api.QueryOptions
}

func (c *consulClient) connect() error {
	if c.kv != nil {
		return nil
	}

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return fmt.Errorf("failed to create consul client: %w", err)
	}

	c.client = client
	c.kv = client.KV()

	return nil
}

// loadRaw returns the raw value stored at key.
func (c *consulClient) loadRaw(ctx context.Context, key string) ([]byte, error) {
	if err := c.connect(); err != nil {
		return nil, err
	}

	pair, _, err := c.kv.Get(key, c.queryOptions.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if pair == nil {
		return nil, nil
	}

	return pair.Value, nil
}

// dynamicValue watches key and returns a channel with the latest value.
//
// The returned channel is closed when ctx is cancelled. The stop function
// stops the underlying watch plan.
func (c *consulClient) dynamicValue(ctx context.Context, wg *sync.WaitGroup, key string) (<-chan []byte, func(), error) {
	if err := c.connect(); err != nil {
		return nil, nil, err
	}

	plan, err := watch.Parse(map[string]interface{}{
		"type": "key",
		"key":  key,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("watch parse: %w", err)
	}

	// unbuffered: only the latest change matters
	vChannel := make(chan []byte)

	plan.HybridHandler = func(_ watch.BlockingParamVal, raw interface{}) {
		if raw == nil {
			return
		}

		if v, ok := raw.(*api.KVPair); ok && v != nil {
			vChannel <- v.Value
		}
	}

	runCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-ctx.Done():
			plan.Stop()
		case <-runCh:
		}

		close(vChannel)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		runCh <- plan.RunWithClientAndHclog(c.client, hclog.NewNullLogger())
	}()

	return vChannel, plan.Stop, nil
}
