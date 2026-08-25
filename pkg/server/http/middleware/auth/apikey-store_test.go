package auth

import (
	"testing"
	"time"
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
