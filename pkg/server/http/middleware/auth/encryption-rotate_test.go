package auth

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestReencryptStaleIntegration(t *testing.T) {
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

	c, err := NewCipher("rotate-old")
	if err != nil {
		t.Fatal(err)
	}

	// a row written with the old key while a rotation runs
	old, err := c.EncryptString("stale-value")
	if err != nil {
		t.Fatal(err)
	}

	const ns = "rotate-stale-test"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO auth_settings (namespace, value_encrypted) VALUES ($1, $2)
		 ON CONFLICT (namespace) DO UPDATE SET value_encrypted = EXCLUDED.value_encrypted`, ns, old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM auth_settings WHERE namespace = $1`, ns) })

	newC, err := NewCipher("rotate-new")
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Rekey("rotate-new"); err != nil {
		t.Fatal(err)
	}

	col := encryptedColumn{table: "auth_settings", idCol: "namespace", valCol: "value_encrypted"}
	if err := reencryptStale(ctx, db, newC, c, col); err != nil {
		t.Fatal(err)
	}

	var sealed string
	if err := db.QueryRowContext(ctx, `SELECT value_encrypted FROM auth_settings WHERE namespace = $1`, ns).Scan(&sealed); err != nil {
		t.Fatal(err)
	}

	if plain, err := newC.DecryptString(sealed); err != nil || plain != "stale-value" {
		t.Fatalf("value after rotation = %q, %v", plain, err)
	}
}
