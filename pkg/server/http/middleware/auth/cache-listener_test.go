package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func TestSignalCacheChangeCoalesces(t *testing.T) {
	changes := make(chan struct{}, 1)

	signalCacheChange(changes)
	signalCacheChange(changes)

	if len(changes) != 1 {
		t.Fatalf("pending changes = %d, want 1", len(changes))
	}
}

func TestAuthListenerErrorHint(t *testing.T) {
	if hint := authListenerErrorHint(fmt.Errorf("wait for auth_changed: %w", io.ErrUnexpectedEOF)); hint == "" {
		t.Fatal("expected an actionable hint for unexpected EOF")
	}
	if hint := authListenerErrorHint(errors.New("permission denied")); hint != "" {
		t.Fatalf("unexpected hint for unrelated error: %q", hint)
	}
}

func TestCacheWatchPollIntervalOverride(t *testing.T) {
	cache := NewCache(nil)
	cache.snap.Store(&Snapshot{Cache: CacheSettings{pollInterval: 7 * time.Second}})

	if got := cache.watchPollInterval(CacheWatchConfig{}); got != 7*time.Second {
		t.Fatalf("shared poll interval = %s, want 7s", got)
	}
	if got := cache.watchPollInterval(CacheWatchConfig{DisableNotificationListener: true}); got != 10*time.Second {
		t.Fatalf("listener-disabled default poll interval = %s, want 10s", got)
	}
	if got := cache.watchPollInterval(CacheWatchConfig{PollInterval: 15 * time.Second}); got != 15*time.Second {
		t.Fatalf("instance poll interval = %s, want 15s", got)
	}
}

func TestCacheNotificationIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := migrate(ctx, db, Migration{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := NewCipher("integration-test-key")
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, cipher)
	cache := NewCache(store)
	if err := cache.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	listenerCtx, stopListener := context.WithCancel(ctx)
	changes := make(chan struct{}, 1)
	listenerDone := make(chan error, 1)
	go func() {
		_, err := cache.listenChangesOnce(listenerCtx, dsn, changes)
		listenerDone <- err
	}()

	// The first signal confirms LISTEN is active and also closes any gap from
	// changes committed while the dedicated connection was being established.
	select {
	case <-changes:
	case <-ctx.Done():
		t.Fatalf("listener did not become ready: %v", ctx.Err())
	}

	const namespace = "it-notification-listener"
	version, err := store.PutSetting(ctx, namespace, json.RawMessage(`{"changed":true}`), "integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.DeleteSetting(context.Background(), namespace) }()

	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("auth_changed notification was not delivered")
	}

	if err := cache.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if cache.Snapshot().Version != version {
		t.Fatalf("cache version = %d, want %d", cache.Snapshot().Version, version)
	}

	stopListener()
	select {
	case err := <-listenerDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("listener shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop after cancellation")
	}
}
