// Package store is the PostgreSQL layer: connection pool, migrations and the
// queries the service needs.
package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadySubscribed = errors.New("user already has a live subscription for this product")
)

type Store struct {
	pool *pgxpool.Pool
}

// Connect opens a pool and waits for the database to answer. Compose starts
// the app and the database together, so the first attempts may fail.
func Connect(ctx context.Context, url string, wait time.Duration) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		pingErr := pool.Ping(ctx)
		if pingErr == nil {
			return &Store{pool: pool}, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("database not reachable: %w", pingErr)
		}
		slog.Warn("waiting for database", "error", pingErr)
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
		}
	}
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Queries runs statements on the pool, outside any transaction.
func (s *Store) Queries() *Queries { return &Queries{db: s.pool} }

// InTx runs fn in a transaction. It commits if fn returns nil and rolls back
// otherwise.
func (s *Store) InTx(ctx context.Context, fn func(q *Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit
	if err := fn(&Queries{db: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// dbtx is what both *pgxpool.Pool and pgx.Tx provide.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Queries struct {
	db dbtx
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
