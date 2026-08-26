package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	authChangeChannel        = "auth_changed"
	authListenRetryMin       = time.Second
	authListenRetryMax       = 30 * time.Second
	authListenStableAfter    = time.Minute
	authNotificationDebounce = 50 * time.Millisecond
)

// listenChanges keeps a dedicated PostgreSQL session subscribed to auth
// changes. LISTEN is session-scoped, so it must not run on a pooled query
// connection. Polling remains the fallback for disconnects and lost events.
func (c *Cache) listenChanges(ctx context.Context, dsn string, changes chan<- struct{}) {
	retry := authListenRetryMin

	for ctx.Err() == nil {
		connectedFor, err := c.listenChangesOnce(ctx, dsn, changes)
		if ctx.Err() != nil {
			return
		}
		if connectedFor >= authListenStableAfter {
			retry = authListenRetryMin
		}

		slog.Error("auth cache notification listener disconnected",
			slog.String("error", err.Error()),
			slog.Duration("retry_in", retry),
		)

		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
		}

		retry = min(retry*2, authListenRetryMax)
	}
}

func (c *Cache) listenChangesOnce(ctx context.Context, dsn string, changes chan<- struct{}) (time.Duration, error) {
	connectedAt := time.Now()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("connect notification listener: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(ctx, "LISTEN "+authChangeChannel); err != nil {
		return time.Since(connectedAt), fmt.Errorf("listen %s: %w", authChangeChannel, err)
	}

	// Recheck the durable version after every connection establishment. This
	// closes the gap where notifications may have been missed while offline.
	signalCacheChange(changes)

	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return time.Since(connectedAt), fmt.Errorf("wait for %s: %w", authChangeChannel, err)
		}

		signalCacheChange(changes)
	}
}

func signalCacheChange(changes chan<- struct{}) {
	select {
	case changes <- struct{}{}:
	default:
		// One pending signal is enough; the receiver checks the durable version.
	}
}
