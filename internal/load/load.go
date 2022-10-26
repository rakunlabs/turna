package load

import (
	"context"

	"github.com/worldline-go/turna/pkg/export"
	"github.com/worldline-go/turna/pkg/load"
)

type Load struct {
	Name     string
	Export   string
	Statics  load.Statics
	Dynamics load.Dynamics
}

func (l *Load) Load(ctx context.Context, save *load.Api) error {
	to := map[string]interface{}{}

	if err := l.Statics.Load(ctx, &to, save); err != nil {
		return err
	}

	if err := l.Dynamics.Load(ctx); err != nil {
		return err
	}

	if l.Export != "" {
		if err := export.File(l.Export, to); err != nil {
			return err
		}
	}

	return nil
}

type Loads []Load

func (l Loads) Load(ctx context.Context) error {
	save := &load.Api{}

	for _, l := range l {
		if err := l.Load(ctx, save); err != nil {
			return err
		}
	}

	return nil
}
