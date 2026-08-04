package session

import (
	"context"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/session/store"
)

func TestSetStoreSessionKey(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		file      string
		redis     string
		wantFile  string
		wantRedis string
	}{
		{
			name:      "inherit top-level key",
			session:   "shared-secret",
			wantFile:  "shared-secret",
			wantRedis: "shared-secret",
		},
		{
			name:      "store-specific keys take precedence",
			session:   "shared-secret",
			file:      "file-secret",
			redis:     "redis-secret",
			wantFile:  "file-secret",
			wantRedis: "redis-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Session{
				SessionKey: tt.session,
				Store: Store{
					Active: "file",
					File: &store.File{
						SessionKey: tt.file,
						Path:       t.TempDir(),
					},
					Redis: &store.Redis{SessionKey: tt.redis},
				},
			}

			if err := m.SetStore(context.Background()); err != nil {
				t.Fatalf("SetStore() error = %v", err)
			}

			if got := m.Store.File.SessionKey; got != tt.wantFile {
				t.Errorf("file session key = %q, want %q", got, tt.wantFile)
			}
			if got := m.Store.Redis.SessionKey; got != tt.wantRedis {
				t.Errorf("redis session key = %q, want %q", got, tt.wantRedis)
			}
		})
	}
}
