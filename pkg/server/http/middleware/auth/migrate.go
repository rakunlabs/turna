package auth

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/rakunlabs/muz"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	defaultMigrationTable   = "auth_migrations"
	defaultMigrationLockKey = "muz:postgres:turna:auth_migrations"
)

// runMigration runs the embedded migrations. When migration.dsn is set, a
// dedicated connection is opened for it and closed afterwards, so DDL
// privileges can be limited to the migration user.
func (m *Auth) runMigration(ctx context.Context, db *sql.DB) error {
	cfg := m.Database.Migration

	if cfg.DSN != "" {
		migrationDB, err := sql.Open("pgx", cfg.DSN)
		if err != nil {
			return fmt.Errorf("open migration database: %w", err)
		}
		defer migrationDB.Close()

		migrationDB.SetMaxOpenConns(1)
		migrationDB.SetMaxIdleConns(1)

		if err := migrationDB.PingContext(ctx); err != nil {
			return fmt.Errorf("ping migration database: %w", err)
		}

		db = migrationDB
	}

	return migrate(ctx, db, cfg)
}

// migrate applies the embedded SQL migrations with muz.
func migrate(ctx context.Context, db *sql.DB, cfg Migration) error {
	m := muz.Migrate{
		Path:      "migrations",
		FS:        migrationsFS,
		Extension: ".sql",
		Values:    cfg.Values,
	}

	table := cfg.Table
	if table == "" {
		table = defaultMigrationTable
	}

	lockKey := cfg.LockKey
	if lockKey == "" {
		lockKey = defaultMigrationLockKey
	}

	driver := &muz.SQLDriver{
		DB:      db,
		Dialect: muz.DialectPostgres,
		Table:   table,
		LockKey: lockKey,
		Logger:  slog.Default(),
	}

	return m.Migrate(ctx, driver)
}
