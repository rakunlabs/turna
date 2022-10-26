package static

import (
	"context"
	"fmt"

	"github.com/worldline-go/igconfig/loader"
)

type File struct {
	Path  string `cfg:"path"`
	Order int    `cfg:"order"`
}

func (l File) Load(ctx context.Context, to interface{}) error {
	c := loader.File{}

	if err := c.LoadWithContext(ctx, l.Path, to); err != nil {
		return fmt.Errorf("failed consul load; %w", err)
	}

	return nil
}

func (l File) GetOrder() int {
	return l.Order
}

func (l *File) SetDefaults() {}
