package store

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/erencivril/paywall-lab/migrations"
)

// Migrate applies the embedded SQL files in file name order. Applied files are
// recorded in schema_migrations and skipped on the next start.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, name := range files {
		err := s.InTx(ctx, func(q *Queries) error {
			// The advisory lock keeps two starting instances from applying
			// the same file at once.
			if _, err := q.db.Exec(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
				return err
			}
			var applied bool
			if err := q.db.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
				return err
			}
			if applied {
				return nil
			}
			sqlText, err := fs.ReadFile(migrations.FS, name)
			if err != nil {
				return err
			}
			if _, err := q.db.Exec(ctx, string(sqlText)); err != nil {
				return fmt.Errorf("run %s: %w", name, err)
			}
			if _, err := q.db.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
				return err
			}
			slog.Info("applied migration", "name", name)
			return nil
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}
