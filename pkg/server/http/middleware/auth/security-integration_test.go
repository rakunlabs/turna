package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

// Uses a real DB to exercise row locks, unique constraints and cross-request MFA.
func TestAuthSecurityIntegration(t *testing.T) {
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
	cipher, err := NewCipher("it-security-key")
	if err != nil {
		t.Fatal(err)
	}
	// Every encrypted record read by this fixture is created below under its own key.
	store := NewStore(db, cipher)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	signingKey := jwtSetting{KID: "security-test", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))}
	newUser := func(t *testing.T) string {
		t.Helper()
		id, err := store.CreateUser(ctx, data.User{Alias: []string{"security-" + ulid.Make().String()}, Details: map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = store.DeleteUser(ctx, id)
			_, _ = db.ExecContext(ctx, `DELETE FROM auth_totp_secrets WHERE user_id=$1`, id)
		})
		return id
	}

	t.Run("TOTP replay and concurrent redemption", func(t *testing.T) {
		id := newUser(t)
		secret, _ := generateTOTPSecret()
		if err := store.UpsertTOTPSecret(ctx, id, secret); err != nil {
			t.Fatal(err)
		}
		if err := store.ConfirmTOTPSecret(ctx, id); err != nil {
			t.Fatal(err)
		}
		code, _ := totpCode(secret, uint64(time.Now().Unix()/totpPeriod))
		var successes atomic.Int32
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				ok, err := store.ConsumeTOTP(ctx, id, code, 1, false)
				if err != nil {
					t.Error(err)
				}
				if ok {
					successes.Add(1)
				}
			})
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("redemptions=%d", successes.Load())
		}
		if err := store.UpsertTOTPSecret(ctx, id, "replacement"); !errors.Is(err, data.ErrConflict) {
			t.Fatalf("confirmed secret replaced: %v", err)
		}
	})

	t.Run("shared MFA budget and recovery codes", func(t *testing.T) {
		id := newUser(t)
		secret, _ := generateTOTPSecret()
		if err := store.UpsertTOTPSecret(ctx, id, secret); err != nil {
			t.Fatal(err)
		}
		if err := store.ConfirmTOTPSecret(ctx, id); err != nil {
			t.Fatal(err)
		}
		codes, hashes, _ := generateTOTPRecoveryCodes()
		if err := store.SetTOTPRecoveryCodes(ctx, id, hashes); err != nil {
			t.Fatal(err)
		}
		for i := range 5 {
			ok, err := store.ConsumeTOTP(ctx, id, "bad-code", 1, false)
			if ok || (i < 4 && err != nil) || (i == 4 && !errors.Is(err, errTOTPLocked)) {
				t.Fatalf("attempt %d: ok=%v err=%v", i, ok, err)
			}
		}
		if _, err := store.ConsumeTOTP(ctx, id, codes[0], 1, false); !errors.Is(err, errTOTPLocked) {
			t.Fatalf("recovery bypassed cooldown: %v", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE auth_totp_secrets SET locked_until=now()-interval '1 second' WHERE user_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if ok, err := store.ConsumeTOTP(ctx, id, codes[0], 1, false); !ok || err != nil {
			t.Fatalf("recovery after cooldown: %v %v", ok, err)
		}
		if ok, err := store.ConsumeTOTP(ctx, id, codes[0], 1, false); ok || err != nil {
			t.Fatalf("recovery replay: %v %v", ok, err)
		}
	})

	t.Run("email MFA challenge preserves first factor", func(t *testing.T) {
		id := newUser(t)
		secret, _ := generateTOTPSecret()
		if err := store.UpsertTOTPSecret(ctx, id, secret); err != nil {
			t.Fatal(err)
		}
		if err := store.ConfirmTOTPSecret(ctx, id); err != nil {
			t.Fatal(err)
		}
		user, err := store.txUserForTest(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		cache := NewCache(store)
		// Isolate the fixture from settings encrypted by unrelated test keys.
		cache.snap.Store(&Snapshot{JWTKey: signingKey, Users: map[string]*data.User{id: user}, Alias: map[string]string{user.Alias[0]: id}, OAuthClients: map[string]AccessClient{"client": {ClientSecret: "secret"}}})
		m := &Auth{store: store, cache: cache}
		emailCode := "security-" + ulid.Make().String()
		hash := hashEmailCode(emailCode)
		if err := store.CreateFlowCode(ctx, flowKindEmail, hash, emailFlow{Alias: user.Alias[0], ClientID: "client"}, time.Minute); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.DeleteFlowCode(ctx, flowKindEmail, hash) })
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
		m.emailCodeGrant(w, r, AccessTokenRequest{Code: emailCode}, "client", "secret")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "mfa_required") {
			t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
		}
		var flow emailFlow
		if err := store.GetFlowCode(ctx, flowKindEmail, hash, &flow); err != nil {
			t.Fatalf("challenge consumed email code: %v", err)
		}
		w = httptest.NewRecorder()
		m.emailCodeGrant(w, r, AccessTokenRequest{Code: emailCode, TOTP: "invalid"}, "client", "secret")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid MFA: %d %s", w.Code, w.Body.String())
		}
		if err := store.GetFlowCode(ctx, flowKindEmail, hash, &flow); err != nil {
			t.Fatal("invalid TOTP consumed the email code")
		}
		code, _ := totpCode(secret, uint64(time.Now().Unix()/totpPeriod))
		w = httptest.NewRecorder()
		m.emailCodeGrant(w, r, AccessTokenRequest{Code: emailCode, TOTP: code}, "client", "secret")
		if w.Code != http.StatusOK {
			t.Fatalf("email+TOTP: %d %s", w.Code, w.Body.String())
		}
		var issued AccessTokenResponse
		if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
			t.Fatal(err)
		}
		if issued.AccessToken == "" {
			t.Fatal("no access token")
		}
		if err := store.GetFlowCode(ctx, flowKindEmail, hash, &flow); !errors.Is(err, data.ErrNotFound) {
			t.Fatalf("successful login did not consume email code: %v", err)
		}

		// Tokens issued before a user was disabled must not remain active on
		// either the userinfo or introspection endpoint.
		disabled := *user
		disabled.Disabled = true
		snapshot := *cache.Snapshot()
		snapshot.Users = map[string]*data.User{id: &disabled}
		cache.snap.Store(&snapshot)
		userinfo := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
		userinfo.Header.Set("Authorization", "Bearer "+issued.AccessToken)
		w = httptest.NewRecorder()
		m.APIUserInfo(w, userinfo)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("disabled userinfo: %d %s", w.Code, w.Body.String())
		}
		form := url.Values{"client_id": {"client"}, "client_secret": {"secret"}, "token": {issued.AccessToken}}
		introspect := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
		introspect.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w = httptest.NewRecorder()
		m.APIIntrospect(w, introspect)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"active":false`) {
			t.Fatalf("disabled introspection: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("normalized aliases are unique across transactions", func(t *testing.T) {
		alias := "Concurrent-" + ulid.Make().String() + "@EXAMPLE.COM"
		var successes atomic.Int32
		var wg sync.WaitGroup
		for i := range 6 {
			wg.Go(func() {
				value := alias
				if i%2 == 0 {
					value = strings.ToLower(value)
				}
				id, err := store.CreateUser(ctx, data.User{Alias: []string{" " + value + " "}})
				if err == nil {
					successes.Add(1)
					t.Cleanup(func() { _ = store.DeleteUser(ctx, id) })
				} else if !errors.Is(err, data.ErrConflict) {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("duplicate aliases created %d accounts", successes.Load())
		}
		// A writer bypassing Store must be constrained too.
		_, err := db.ExecContext(ctx, `INSERT INTO auth_users(id, alias) VALUES ($1,$2::text[])`, "direct-"+ulid.Make().String(), `{"`+strings.ToUpper(alias)+`"}`)
		if err == nil {
			t.Fatal("direct SQL bypassed the alias uniqueness constraint")
		}
	})

	t.Run("profile field patch preserves intervening directory updates", func(t *testing.T) {
		id := newUser(t)
		before, err := store.txUserForTest(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateUserLDAP(ctx, id, before.Alias, map[string]any{"uid": "updated-by-ldap", "name": "Directory Name", "custom": "preserve"}, []string{"new-ldap-role"}); err != nil {
			t.Fatal(err)
		}
		description := "Operator note"
		if err := store.PatchUser(ctx, id, data.UserPatch{Description: &description, DetailsPatch: map[string]any{"name": "Edited Name"}}); err != nil {
			t.Fatal(err)
		}
		after, err := store.txUserForTest(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if after.Details["uid"] != "updated-by-ldap" || after.Details["name"] != "Edited Name" || after.Details["custom"] != "preserve" || len(after.SyncRoleIDs) != 1 || after.SyncRoleIDs[0] != "new-ldap-role" || after.Description != description {
			t.Fatalf("patch overwrote concurrent state: %+v", after)
		}
		if err := store.PatchUser(ctx, id, data.UserPatch{DetailsPatch: map[string]any{"custom": nil}}); err != nil {
			t.Fatal(err)
		}
		after, err = store.txUserForTest(ctx, id)
		if err != nil || after.Details["custom"] != nil || after.Details["uid"] != "updated-by-ldap" {
			t.Fatalf("field removal replaced other details: %+v %v", after, err)
		}
	})

	t.Run("alias migration refuses to merge existing accounts", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		schema := "alias_test_" + strings.ToLower(ulid.Make().String())
		if _, err := tx.ExecContext(ctx, `CREATE SCHEMA `+schema+`; SET LOCAL search_path TO `+schema); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE auth_users(id text PRIMARY KEY, alias text[], doc jsonb DEFAULT '{}'); INSERT INTO auth_users(id,alias) VALUES ('one',ARRAY['User@example.com']),('two',ARRAY['user@EXAMPLE.COM'])`); err != nil {
			t.Fatal(err)
		}
		migration, err := migrationsFS.ReadFile("migrations/14_user_aliases.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, string(migration))
		if err == nil || !strings.Contains(err.Error(), "Duplicate normalized auth alias") {
			t.Fatalf("collision migration: %v", err)
		}
	})

	t.Run("secrets are write only and round trip safely", func(t *testing.T) {
		ns := "security-" + ulid.Make().String()
		t.Cleanup(func() { _, _ = store.DeleteSetting(ctx, ns) })
		if _, err := store.PutSetting(ctx, ns, json.RawMessage(`{"smtp":{"password":"smtp-secret","host":"old"},"private_key":"key-secret"}`), "it"); err != nil {
			t.Fatal(err)
		}
		m := &Auth{store: store}
		r := httptest.NewRequest(http.MethodGet, "/v1/settings/"+ns, nil)
		r.SetPathValue("namespace", ns)
		w := httptest.NewRecorder()
		m.GetSetting(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "smtp-secret") || strings.Contains(w.Body.String(), "key-secret") || !strings.Contains(w.Body.String(), `"password_set":true`) {
			t.Fatalf("secret GET: %d %s", w.Code, w.Body.String())
		}
		if _, err := store.PutSetting(ctx, ns, json.RawMessage(`{"smtp":{"password":"","host":"new","password_set":true},"private_key":""}`), "it"); err != nil {
			t.Fatal(err)
		}
		setting, err := store.GetSetting(ctx, ns)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(setting.Value), "smtp-secret") || !strings.Contains(string(setting.Value), "key-secret") || strings.Contains(string(setting.Value), "password_set") {
			t.Fatalf("secrets lost or flags stored: %s", setting.Value)
		}
		if _, err := store.PutSetting(ctx, ns, json.RawMessage(`{"smtp":{"password":null}}`), "it"); err != nil {
			t.Fatal(err)
		}
		setting, err = store.GetSetting(ctx, ns)
		if err != nil || strings.Contains(string(setting.Value), "smtp-secret") {
			t.Fatalf("explicit null did not clear secret: %s %v", setting.Value, err)
		}

		clientID := "security-client-" + ulid.Make().String()
		t.Cleanup(func() { _, _ = store.DeleteOAuthClient(ctx, clientID) })
		if _, err := store.PutOAuthClient(ctx, clientID, json.RawMessage(`{"client_secret":"client-secret","scope":["openid"]}`), true, "it"); err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(http.MethodGet, "/v1/oauth/clients/"+clientID, nil)
		r.SetPathValue("id", clientID)
		w = httptest.NewRecorder()
		m.GetOAuthClient(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"client-secret"`) || !strings.Contains(w.Body.String(), `"client_secret_set":true`) {
			t.Fatalf("client GET: %d %s", w.Code, w.Body.String())
		}
		if _, err := store.PutOAuthClient(ctx, clientID, json.RawMessage(`{"client_secret":"","scope":["profile"]}`), true, "it"); err != nil {
			t.Fatal(err)
		}
		client, err := store.GetOAuthClient(ctx, clientID)
		if err != nil || !strings.Contains(string(client.Config), `"client-secret"`) {
			t.Fatalf("client secret lost: %v", err)
		}
	})
}

// Reads one fixture user without loading records belonging to other test keys.
func (s *Store) txUserForTest(ctx context.Context, id string) (*data.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	return s.txUserByID(ctx, tx, id)
}
