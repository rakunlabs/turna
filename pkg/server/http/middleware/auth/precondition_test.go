package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

func conditionContext(t *testing.T, header, value string) context.Context {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/", nil)
	r.Header.Set(header, value)
	conditional, err := conditionalRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	return conditional.Context()
}

func TestWriteConditionHeaders(t *testing.T) {
	for _, match := range []string{`W/"rev-1"`, `"rev-1", "rev-2"`, `*`, `"other"`} {
		r := httptest.NewRequest(http.MethodPut, "/", nil)
		r.Header.Set("If-Match", match)
		if _, err := conditionalRequest(r); err == nil {
			t.Errorf("invalid precondition accepted: %s", match)
		}
	}
}

func TestRecordRevisionsIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}
	ctx := t.Context()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(ctx, db, Migration{}); err != nil {
		t.Fatal(err)
	}
	cipher, err := NewCipher("revision-fixtures")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(db, cipher)
	m := &Auth{store: s, cache: NewCache(s)}
	unique := strings.ToLower(ulid.Make().String())
	match := func(revision string) context.Context { return conditionContext(t, "If-Match", `"`+revision+`"`) }
	createOnly := conditionContext(t, "If-None-Match", "*")

	t.Run("API keys reject stale updates and deletes", func(t *testing.T) {
		id, err := s.CreateAPIKey(ctx, APIKeyMeta{Name: "Original", RoleIDs: []string{"role"}}, "revision-key-"+unique, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeleteAPIKeyByID(ctx, id) //nolint:errcheck
		meta, err := s.GetAPIKeyMeta(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		revision := "keyrev-" + strconv.FormatInt(meta.Revision, 10)
		conditional := match(revision)
		var successes, conflicts atomic.Int32
		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				name := "Winner"
				err := s.UpdateAPIKeyByID(conditional, id, APIKeyUpdate{Name: &name})
				if err == nil {
					successes.Add(1)
				} else if errors.Is(err, errPrecondition) {
					conflicts.Add(1)
				} else {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if successes.Load() != 1 || conflicts.Load() != 4 {
			t.Fatalf("API key writes=%d conflicts=%d", successes.Load(), conflicts.Load())
		}
		if err := s.DeleteAPIKeyByID(conditional, id); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale API key deletion: %v", err)
		}
		// The actual HTTP handler must turn conflicts into 412, not 500.
		r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"Stale"}`))
		r.SetPathValue("id", id)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"`+revision+`"`)
		w := httptest.NewRecorder()
		m.adminOnly(m.UpdateAPIKeyPrincipalAPI)(w, r)
		if w.Code != 412 {
			t.Fatalf("API key HTTP %d %s", w.Code, w.Body.String())
		}
		// Internal read/merge/write also uses CAS, without an HTTP header.
		name := "Fresh"
		if err := s.UpdateAPIKeyByID(ctx, id, APIKeyUpdate{Name: &name}); err != nil {
			t.Fatal(err)
		}
		meta.Name = "Stale internal snapshot"
		if err := s.updateAPIKeyMeta(ctx, meta, false); !errors.Is(err, errPrecondition) {
			t.Fatalf("internal stale snapshot overwrote key: %v", err)
		}
		latest, err := s.GetAPIKeyMeta(ctx, id)
		if err != nil || latest.Name != "Fresh" {
			t.Fatalf("stored key: %+v %v", latest, err)
		}
	})

	t.Run("self-service API key headers are enforced and owner-scoped", func(t *testing.T) {
		ownerID, err := s.CreateUser(ctx, data.User{Alias: []string{"key-owner-" + unique}})
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeleteUser(ctx, ownerID) //nolint:errcheck
		m.cache.snap.Store(&Snapshot{Users: map[string]*data.User{ownerID: {ID: ownerID, Alias: []string{"key-owner-" + unique}}}, Alias: map[string]string{"key-owner-" + unique: ownerID}, APIKey: APIKeySettings{SelfService: true}})
		defer m.cache.snap.Store(&Snapshot{})
		id, err := s.CreateAPIKey(ctx, APIKeyMeta{UserID: ownerID, Name: "Owned"}, "owned-key-"+unique, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeleteAPIKeyByID(ctx, id) //nolint:errcheck
		meta, err := s.GetAPIKeyMeta(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		tag := `"keyrev-` + strconv.FormatInt(meta.Revision, 10) + `"`
		if err := s.DeleteAPIKey(conditionContext(t, "If-Match", tag), "different-owner", id); !errors.Is(err, errPrecondition) {
			t.Fatalf("cross-owner deletion: %v", err)
		}
		call := func() *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"description":"Updated"}`))
			r.SetPathValue("id", id)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User", "key-owner-"+unique)
			r.Header.Set("If-Match", tag)
			w := httptest.NewRecorder()
			m.apiKeySelfOrAdmin(m.UpdateAPIKeyAPI)(w, r)
			return w
		}
		if w := call(); w.Code != 200 {
			t.Fatalf("self update %d %s", w.Code, w.Body.String())
		}
		if w := call(); w.Code != 412 {
			t.Fatalf("stale self update %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("migration assigns revisions to existing records", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		schema := "revision_upgrade_" + unique
		if _, err := tx.ExecContext(ctx, `CREATE SCHEMA `+schema+`; SET LOCAL search_path TO `+schema); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"auth_settings", "auth_oauth_clients", "auth_oauth_providers", "auth_ldap_configs", "auth_saml_providers", "auth_users", "auth_roles", "auth_permissions", "auth_lmaps"} {
			if _, err := tx.ExecContext(ctx, `CREATE TABLE `+table+` (id text PRIMARY KEY, doc jsonb NOT NULL DEFAULT '{}', config jsonb NOT NULL DEFAULT '{}'); INSERT INTO `+table+`(id) VALUES ('existing')`); err != nil {
				t.Fatal(err)
			}
		}
		migration, err := migrationsFS.ReadFile("migrations/15_record_revisions.sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
		var revision, embedded string
		if err := tx.QueryRowContext(ctx, `SELECT revision,doc->>'revision' FROM auth_users WHERE id='existing'`).Scan(&revision, &embedded); err != nil {
			t.Fatal(err)
		}
		if revision == "" || revision != embedded {
			t.Fatalf("upgraded row revision %q embedded %q", revision, embedded)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE auth_users SET doc='{"revision":"forged"}' WHERE id='existing'`); err != nil {
			t.Fatal(err)
		}
		var next string
		if err := tx.QueryRowContext(ctx, `SELECT doc->>'revision' FROM auth_users WHERE id='existing'`).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if next == revision || next == "forged" {
			t.Fatalf("revision was reused or supplied by caller: %s", next)
		}
	})

	t.Run("settings serialize competing writes and reject recreate ABA", func(t *testing.T) {
		ns := "revision-" + unique
		defer s.DeleteSetting(ctx, ns) //nolint:errcheck
		if _, err := s.PutSetting(createOnly, ns, json.RawMessage(`{"value":"original"}`), "one"); err != nil {
			t.Fatal(err)
		}
		original, err := s.GetSetting(ctx, ns)
		if err != nil || original.Revision == "" {
			t.Fatalf("initial revision: %+v %v", original, err)
		}
		if _, err := s.PutSetting(createOnly, ns, json.RawMessage(`{"value":"replace"}`), "two"); !errors.Is(err, errPrecondition) {
			t.Fatalf("create-only replaced record: %v", err)
		}
		conditional := match(original.Revision)
		var successes, conflicts atomic.Int32
		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() {
				_, err := s.PutSetting(conditional, ns, json.RawMessage(`{"value":"winner"}`), "writer")
				if err == nil {
					successes.Add(1)
				} else if errors.Is(err, errPrecondition) {
					conflicts.Add(1)
				} else {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if successes.Load() != 1 || conflicts.Load() != 5 {
			t.Fatalf("writes=%d conflicts=%d", successes.Load(), conflicts.Load())
		}
		latest, err := s.GetSetting(ctx, ns)
		if err != nil || latest.Revision == original.Revision {
			t.Fatalf("revision not advanced: %+v %v", latest, err)
		}
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"value":{"issuer":"stale"}}`))
		r.SetPathValue("namespace", ns)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"`+original.Revision+`"`)
		w := httptest.NewRecorder()
		m.adminOnly(m.PutSetting)(w, r)
		if w.Code != 412 {
			t.Fatalf("stale settings HTTP response: %d %s", w.Code, w.Body.String())
		}
		// A different namespace write must not invalidate this record.
		other := ns + "-other"
		defer s.DeleteSetting(ctx, other) //nolint:errcheck
		if _, err := s.PutSetting(ctx, other, json.RawMessage(`{}`), "other"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutSetting(match(latest.Revision), ns, json.RawMessage(`{"value":"next"}`), "one"); err != nil {
			t.Fatalf("unrelated write caused conflict: %v", err)
		}
		if _, err := s.DeleteSetting(ctx, ns); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutSetting(createOnly, ns, json.RawMessage(`{}`), "recreated"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PutSetting(match(original.Revision), ns, json.RawMessage(`{}`), "stale"); !errors.Is(err, errPrecondition) {
			t.Fatalf("delete/recreate reused revision: %v", err)
		}
	})

	t.Run("configuration HTTP returns ETag and 412 without leaking secrets", func(t *testing.T) {
		id := "revision-client-" + unique
		defer s.DeleteOAuthClient(ctx, id) //nolint:errcheck
		if _, err := s.PutOAuthClient(ctx, id, json.RawMessage(`{"client_secret":"keep-secret","scope":["openid"]}`), true, "one"); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		m.adminOnly(m.GetOAuthClient)(w, r)
		etag := w.Header().Get("ETag")
		if w.Code != 200 || etag == "" || strings.Contains(w.Body.String(), "keep-secret") {
			t.Fatalf("GET %d %s etag=%s", w.Code, w.Body.String(), etag)
		}
		write := func(tag string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"config":{"client_secret":"","scope":["profile"]},"enabled":true}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("If-Match", tag)
			r.SetPathValue("id", id)
			w := httptest.NewRecorder()
			m.adminOnly(m.PutOAuthClient)(w, r)
			return w
		}
		if w := write(etag); w.Code != 200 {
			t.Fatalf("first PUT %d %s", w.Code, w.Body.String())
		}
		if w := write(etag); w.Code != 412 {
			t.Fatalf("stale PUT %d %s", w.Code, w.Body.String())
		}
		stored, err := s.GetOAuthClient(ctx, id)
		if err != nil || !strings.Contains(string(stored.Config), "keep-secret") {
			t.Fatalf("credential lost: %+v %v", stored, err)
		}
		if _, err := s.DeleteOAuthClient(conditionContext(t, "If-Match", etag), id); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale delete accepted: %v", err)
		}
		if _, err := s.GetOAuthClient(ctx, id); err != nil {
			t.Fatalf("failed delete removed record: %v", err)
		}
	})

	t.Run("LDAP and direct SQL invalidate stale user drafts", func(t *testing.T) {
		id, err := s.CreateUser(ctx, data.User{Alias: []string{"revision-user-" + unique}, Details: map[string]any{"name": "Original"}})
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeleteUser(ctx, id) //nolint:errcheck
		before, err := s.txUserForTest(ctx, id)
		if err != nil || before.Revision == "" {
			t.Fatalf("user revision: %+v %v", before, err)
		}
		if err := s.UpdateUserSyncRoles(ctx, id, []string{"directory-role"}); err != nil {
			t.Fatal(err)
		}
		note := "stale edit"
		if err := s.PatchUser(match(before.Revision), id, data.UserPatch{Description: &note}); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale patch accepted: %v", err)
		}
		after, err := s.txUserForTest(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if after.Description != "" || len(after.SyncRoleIDs) != 1 {
			t.Fatalf("stale patch changed user: %+v", after)
		}
		if _, err := db.ExecContext(ctx, `UPDATE auth_users SET disabled=true WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"description":"stale HTTP edit"}`))
		r.SetPathValue("id", id)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"`+after.Revision+`"`)
		w := httptest.NewRecorder()
		m.adminOnly(m.PatchUserAPI)(w, r)
		if w.Code != 412 {
			t.Fatalf("stale user HTTP response: %d %s", w.Code, w.Body.String())
		}
		if err := s.PutUser(match(after.Revision), *after); !errors.Is(err, errPrecondition) {
			t.Fatalf("direct SQL change overwritten: %v", err)
		}
		// Existing header-less integrations are deliberately backward-compatible.
		if err := s.PatchUser(ctx, id, data.UserPatch{Description: &note}); err != nil {
			t.Fatalf("legacy caller broke: %v", err)
		}
	})

	t.Run("role permission and map revisions survive cache serialization", func(t *testing.T) {
		roleID, err := s.CreateRole(ctx, data.Role{Name: "revision-role-" + unique})
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeleteRole(ctx, roleID) //nolint:errcheck
		roles, err := s.LoadRoles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var role *data.Role
		for _, r := range roles {
			if r.ID == roleID {
				role = r
			}
		}
		if role == nil || role.Revision == "" {
			t.Fatal("role revision missing")
		}
		if err := s.PutRole(match(role.Revision), *role); err != nil {
			t.Fatal(err)
		}
		if err := s.PutRole(match(role.Revision), *role); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale role accepted: %v", err)
		}
		permissionID, err := s.CreatePermission(ctx, data.Permission{Name: "revision-permission-" + unique})
		if err != nil {
			t.Fatal(err)
		}
		defer s.DeletePermission(ctx, permissionID) //nolint:errcheck
		perms, err := s.LoadPermissions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var perm *data.Permission
		for _, p := range perms {
			if p.ID == permissionID {
				perm = p
			}
		}
		if perm == nil || perm.Revision == "" {
			t.Fatal("permission revision missing")
		}
		if err := s.PutPermission(match(perm.Revision), *perm); err != nil {
			t.Fatal(err)
		}
		if err := s.PutPermission(match(perm.Revision), *perm); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale permission accepted: %v", err)
		}
		name := "revision-map-" + unique
		if err := s.CreateLMap(ctx, data.LMap{Name: name, RoleIDs: []string{}}); err != nil {
			t.Fatal(err)
		}
		defer s.DeleteLMap(ctx, name) //nolint:errcheck
		maps, err := s.LoadLMaps(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var lmap *data.LMap
		for _, item := range maps {
			if item.Name == name {
				lmap = item
			}
		}
		if lmap == nil || lmap.Revision == "" {
			t.Fatal("map revision missing")
		}
		if err := s.PutLMap(match(lmap.Revision), *lmap); err != nil {
			t.Fatal(err)
		}
		if err := s.PutLMap(match(lmap.Revision), *lmap); !errors.Is(err, errPrecondition) {
			t.Fatalf("stale map accepted: %v", err)
		}
	})
}
