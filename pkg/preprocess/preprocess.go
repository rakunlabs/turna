package preprocess

import (
	"context"
	"fmt"

	"github.com/rakunlabs/turna/pkg/preprocess/replace"
)

type Configs []Config

type Config struct {
	Replace *replace.Config `cfg:"replace"`
}

type Runner interface {
	Run(ctx context.Context) error
}

func (c Config) Get() Runner {
	if c.Replace != nil {
		return c.Replace
	}

	return nil
}

func (c Configs) Run(ctx context.Context) error {
	for _, config := range c {
		if err := config.Get().Run(ctx); err != nil {
			return fmt.Errorf("preprocess run failed: %w", err)
		}
	}

	return nil
}
