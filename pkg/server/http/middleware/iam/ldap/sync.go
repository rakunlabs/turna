package ldap

import (
	"context"
	"log/slog"
	"time"
)

func (l *Ldap) StartSync(ctx context.Context, fn func(bool, string) error) {
	if l.DisableSync {
		return
	}

	if err := fn(false, ""); err != nil {
		slog.Error("failed to sync ldap", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.SyncDuration):
			if err := fn(false, ""); err != nil {
				slog.Error("failed to sync ldap", slog.String("error", err.Error()))
			}
		}
	}
}
