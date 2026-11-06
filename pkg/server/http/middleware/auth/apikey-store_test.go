package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

func TestShouldRefreshAPIKeyLastUsed(t *testing.T) {
	store := &Store{}
	now := time.Now()

	if !store.shouldRefreshAPIKeyLastUsed("key-1", now) {
		t.Fatal("first use should refresh last_used_at")
	}
	if store.shouldRefreshAPIKeyLastUsed("key-1", now.Add(apiKeyLastUsedInterval-time.Second)) {
		t.Fatal("use inside the interval should not refresh last_used_at")
	}
	if !store.shouldRefreshAPIKeyLastUsed("key-1", now.Add(apiKeyLastUsedInterval)) {
		t.Fatal("use at the interval boundary should refresh last_used_at")
	}
	if !store.shouldRefreshAPIKeyLastUsed("key-2", now) {
		t.Fatal("a different key should have its own refresh interval")
	}
}

func TestApplyAPIKeyUpdateDescription(t *testing.T) {
	meta := APIKeyMeta{Description: "old description"}
	description := "new description"

	applyAPIKeyUpdate(&meta, APIKeyUpdate{Description: &description})

	if meta.Description != description {
		t.Fatalf("description = %q", meta.Description)
	}
}

func TestCheckOwnerAPIKeyUpdateDisabled(t *testing.T) {
	enable, disable := false, true

	if err := checkOwnerAPIKeyUpdate(&APIKeyMeta{Disabled: true}, APIKeyUpdate{Disabled: &enable}); !errors.Is(err, data.ErrInvalidRequest) {
		t.Fatalf("owner re-enabling a disabled key: err = %v", err)
	}
	if err := checkOwnerAPIKeyUpdate(&APIKeyMeta{Disabled: true}, APIKeyUpdate{Disabled: &disable}); err != nil {
		t.Fatalf("keeping a key disabled: %v", err)
	}
	if err := checkOwnerAPIKeyUpdate(&APIKeyMeta{Disabled: true}, APIKeyUpdate{}); err != nil {
		t.Fatalf("update without disabled: %v", err)
	}
	if err := checkOwnerAPIKeyUpdate(&APIKeyMeta{}, APIKeyUpdate{Disabled: &disable}); err != nil {
		t.Fatalf("owner disabling a key: %v", err)
	}
}
