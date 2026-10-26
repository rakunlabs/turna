package auth

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestRefreshReuseIntegration runs against a real PostgreSQL when
// AUTH_TEST_DSN is set.
func TestRefreshReuseIntegration(t *testing.T) {
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

	if err := migrate(ctx, db, Migration{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cipher, err := NewCipher("integration-test-key")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	m := &Auth{store: NewStore(db, cipher)}

	suffix := time.Now().Format("150405.000000000")
	jti, sid := "reuse-jti-"+suffix, "reuse-sid-"+suffix
	claims := jwt.MapClaims{"jti": jti, "sid": sid, "sub": "user", "azp": "ui", "session_exp": time.Now().Add(time.Hour).Unix()}
	exp := time.Now().Add(time.Hour)

	// first use, then a racing second use inside the grace window
	for i := range 2 {
		reused, err := m.refreshReused(ctx, claims, jti, sid, exp, time.Minute)
		if err != nil || reused {
			t.Fatalf("use %d: reused=%v err=%v", i, reused, err)
		}
	}
	if revoked, err := m.claimsRevoked(ctx, claims); err != nil || revoked {
		t.Fatalf("grace reuse revoked the session: revoked=%v err=%v", revoked, err)
	}

	// replay after the window revokes the whole family
	time.Sleep(1100 * time.Millisecond)
	reused, err := m.refreshReused(ctx, claims, jti, sid, exp, 500*time.Millisecond)
	if err != nil || !reused {
		t.Fatalf("late replay: reused=%v err=%v", reused, err)
	}

	sibling := jwt.MapClaims{"jti": "other-" + suffix, "sid": sid}
	if revoked, err := m.claimsRevoked(ctx, sibling); err != nil || !revoked {
		t.Fatalf("session family not revoked: revoked=%v err=%v", revoked, err)
	}
}
