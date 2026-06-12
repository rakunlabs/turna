package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

// TestStoreIntegration runs against a real PostgreSQL when AUTH_TEST_DSN is set.
//
//	AUTH_TEST_DSN="postgres://turna:turna@localhost:15432/turna?sslmode=disable" go test ./pkg/server/http/middleware/auth -run TestStoreIntegration -v
func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	if err := migrate(ctx, db, Migration{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cipher, err := NewCipher("integration-test-key")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	store := NewStore(db, cipher)
	cache := NewCache(store)

	// check settings now live in the database
	if _, err := store.PutSetting(ctx, "check", json.RawMessage(`{"no_host_check":true}`), "integration"); err != nil {
		t.Fatalf("put check setting: %v", err)
	}

	if err := cache.Reload(ctx); err != nil {
		t.Fatalf("cache reload: %v", err)
	}

	userCtx := data.WithContextUserName(ctx, "integration")

	// permission
	permID, err := store.CreatePermission(userCtx, data.Permission{
		Name: "it-perm",
		Resources: []data.Resource{
			{Methods: []string{"*"}, Paths: []string{"/it/**"}},
		},
		Scope: map[string][]string{"openid": {"it-admin"}},
	})
	if err != nil {
		t.Fatalf("create permission: %v", err)
	}

	// role
	roleID, err := store.CreateRole(userCtx, data.Role{
		Name:          "it-role",
		PermissionIDs: []string{permID},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	// user
	userID, err := store.CreateUser(userCtx, data.User{
		Alias:   []string{"it-user@example.com"},
		RoleIDs: []string{roleID},
		Local:   true,
		Details: map[string]any{"name": "IT User", "password": "secret-password"},
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// alias conflict
	if _, err := store.CreateUser(userCtx, data.User{Alias: []string{"it-user@example.com"}}); err == nil {
		t.Fatal("expected alias conflict")
	}

	if err := cache.Reload(ctx); err != nil {
		t.Fatalf("cache reload: %v", err)
	}

	// check engine against postgres-loaded snapshot
	resp, err := cache.Check(data.CheckRequest{Alias: "it-user@example.com", Path: "/it/x", Method: "POST"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !resp.Allowed {
		t.Fatal("expected check to allow")
	}

	// password must be hashed
	user, err := cache.GetUser(data.GetUserRequest{ID: userID})
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if pw, _ := user.Details["password"].(string); pw == "secret-password" || pw == "" {
		t.Fatalf("expected hashed password, got %q", pw)
	}
	if err := compareBcryptBase64(user.Details["password"].(string), "secret-password"); err != nil {
		t.Fatalf("bcrypt compare failed: %v", err)
	}

	// patch user
	if err := store.PatchUser(userCtx, userID, data.UserPatch{IsActive: &data.False}); err != nil {
		t.Fatalf("patch user: %v", err)
	}
	_ = cache.Reload(ctx)

	resp, _ = cache.Check(data.CheckRequest{Alias: "it-user@example.com", Path: "/it/x", Method: "POST"})
	if resp.Allowed {
		t.Fatal("expected disabled user to be denied")
	}

	// settings + oauth client
	if _, err := store.PutSetting(ctx, "it-setting", json.RawMessage(`{"a":1}`), "integration"); err != nil {
		t.Fatalf("put setting: %v", err)
	}
	setting, err := store.GetSetting(ctx, "it-setting")
	if err != nil || string(setting.Value) != `{"a":1}` {
		t.Fatalf("get setting: %v %s", err, setting.Value)
	}

	if _, err := store.PutOAuthClient(ctx, "it-client", json.RawMessage(`{"client_secret":"s","scope":["openid"]}`), true, "integration"); err != nil {
		t.Fatalf("put oauth client: %v", err)
	}
	_ = cache.Reload(ctx)

	if client, ok := cache.Snapshot().OAuthClients["it-client"]; !ok || client.ClientSecret != "s" {
		t.Fatalf("expected decrypted oauth client in snapshot, got %+v", client)
	}

	// lmap ensure
	if err := store.EnsureLMaps(ctx, []data.LMapCheckCreate{{Name: "it-group"}}); err != nil {
		t.Fatalf("ensure lmaps: %v", err)
	}
	_ = cache.Reload(ctx)

	lmap, err := cache.GetLMap("it-group")
	if err != nil || len(lmap.RoleIDs) != 1 {
		t.Fatalf("expected lmap with one role, got %v %v", lmap, err)
	}

	// role delete cleans user references
	if err := store.DeleteRole(userCtx, roleID); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	_ = cache.Reload(ctx)

	user, err = cache.GetUser(data.GetUserRequest{ID: userID})
	if err != nil {
		t.Fatalf("get user after role delete: %v", err)
	}
	if len(user.RoleIDs) != 0 {
		t.Fatalf("expected role removed from user, got %v", user.RoleIDs)
	}

	// version increments and events recorded
	version, err := store.Version(ctx)
	if err != nil || version < 5 {
		t.Fatalf("expected version to grow, got %d (%v)", version, err)
	}

	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_events`).Scan(&eventCount); err != nil || eventCount == 0 {
		t.Fatalf("expected events, got %d (%v)", eventCount, err)
	}

	// cleanup for re-runs
	if err := store.DeleteUser(userCtx, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if err := store.DeletePermission(userCtx, permID); err != nil {
		t.Fatalf("delete permission: %v", err)
	}
	if err := store.DeleteLMap(userCtx, "it-group"); err != nil {
		t.Fatalf("delete lmap: %v", err)
	}
	if _, err := store.DeleteSetting(ctx, "it-setting"); err != nil {
		t.Fatalf("delete setting: %v", err)
	}
	if _, err := store.DeleteOAuthClient(ctx, "it-client"); err != nil {
		t.Fatalf("delete oauth client: %v", err)
	}
	// it-group role left by EnsureLMaps cleanup
	sn := cache.Snapshot()
	if id, ok := sn.RoleNames["it-group"]; ok {
		_ = store.DeleteRole(userCtx, id)
	}
}
