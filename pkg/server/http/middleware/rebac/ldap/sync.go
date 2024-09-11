package ldap

import (
	"context"
	"log/slog"
	"time"
)

func (l *Ldap) StartSync(ctx context.Context, fn func() error) {
	if err := fn(); err != nil {
		slog.Error("failed to sync ldap", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.SyncDuration):
			if err := fn(); err != nil {
				slog.Error("failed to sync ldap", slog.String("error", err.Error()))
			}
		}
	}
}
