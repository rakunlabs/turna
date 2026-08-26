package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
