package openfga

import "context"

var SQLMigration = []string{
	`CREATE TABLE IF NOT EXISTS user_list
(
	id    text  not null
		constraint user_list_pk
			primary key,
	alias jsonb not null,
	details jsonb
);

create index IF NOT EXISTS user_list_alias_index
	on user_list USING gin (alias);`,
}

type Database struct {
	Postgres string `cfg:"postgres"`
}

func (m *OpenFGA) Migration(ctx context.Context) error {
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	for _, query := range SQLMigration {
		_, err = tx.Exec(query)
		if err != nil {
			_ = tx.Rollback()

			return err
		}
	}

	return tx.Commit()
}
