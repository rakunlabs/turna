package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestEncryptionUpgradeIntegration starts on a legacy database (enc:v1 values,
// no kdf salt), upgrades it to argon2id and restarts on the upgraded data.
func TestEncryptionUpgradeIntegration(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("AUTH_TEST_DSN is not set")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// a dedicated database: the upgrade re-seals every row of every table
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	dbName := "turna_upgrade_" + strings.ToLower(strings.ReplaceAll(time.Now().Format("150405.000000"), ".", ""))
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Skipf("cannot create test database: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + dbName + ` WITH (FORCE)`) })

	dsn, err = withDatabase(dsn, dbName)
	if err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := migrate(ctx, db, Migration{}); err != nil {
		t.Fatal(err)
	}

	const key = "upgrade-test-key"
	const ns = "upgrade-kdf-test"

	// legacy state: enc:v1 canary and value, no salt
	legacy, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	canary, _ := legacy.EncryptString(encryptionCanary)
	value, _ := legacy.EncryptString(`{"secret":"v"}`)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM auth_encryption_check`, nil},
		{`INSERT INTO auth_encryption_check (id, canary_encrypted) VALUES (true, $1)`, []any{canary}},
		{`INSERT INTO auth_settings (namespace, value_encrypted) VALUES ($1, $2)
		  ON CONFLICT (namespace) DO UPDATE SET value_encrypted = EXCLUDED.value_encrypted`, []any{ns, value}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM auth_settings WHERE namespace = $1`, ns)
		_, _ = db.Exec(`DELETE FROM auth_encryption_check`)
	})

	start := func() *Auth {
		t.Helper()

		runCtx, stop := context.WithCancel(ctx)
		t.Cleanup(stop)

		m := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: key}}
		if _, err := m.Middleware(runCtx, "auth-upgrade-"+t.Name()); err != nil {
			t.Fatalf("middleware: %v", err)
		}

		return m
	}

	// each start runs with its own context so DROP DATABASE can terminate it
	m := start()
	if len(m.cipher.Salt()) != 0 {
		t.Fatal("legacy database must start without a salt")
	}

	upgraded, err := m.upgradeEncryption(ctx)
	if err != nil || !upgraded {
		t.Fatalf("upgrade = %v, %v", upgraded, err)
	}

	var stored string
	if err := db.QueryRowContext(ctx, `SELECT value_encrypted FROM auth_settings WHERE namespace = $1`, ns).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, encryptedPrefixV2) {
		t.Fatalf("value not upgraded: %q", stored[:7])
	}

	if again, err := m.upgradeEncryption(ctx); err != nil || again {
		t.Fatalf("second upgrade = %v, %v, want no-op", again, err)
	}

	// a restart loads the salt and reads the upgraded data
	restarted := start()
	if len(restarted.cipher.Salt()) == 0 {
		t.Fatal("restart must load the kdf salt")
	}
	raw, err := restarted.store.GetSettingValue(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil || got["secret"] != "v" {
		t.Fatalf("setting after restart = %s, %v", raw, err)
	}

	// a wrong key still fails fast
	wrong := &Auth{PrefixPath: "/auth", Database: Database{DSN: dsn}, Encryption: Encryption{Key: "other-key"}}
	if _, err := wrong.Middleware(ctx, "auth-upgrade-wrong"); err == nil || !strings.Contains(err.Error(), "encryption key mismatch") {
		t.Fatalf("wrong key err = %v", err)
	}
}

func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name

	return u.String(), nil
}
