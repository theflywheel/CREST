// Package store is the only place in CREST that knows what a database is.
//
// Everything else asks for the small surface below. That is enforced by
// depguard, not by convention: once two packages open connections, "what could
// this service have written" stops having an answer, and on a system whose
// records decide whether someone gets paid that question has to keep one.
//
// Each service owns a schema and nothing else. There are no cross-schema
// foreign keys — services talk over HTTP, as they do in production, and a
// foreign key between two services is a distributed system that only works
// when it isn't distributed.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/theflywheel/crest/pkg/clock"
)

// ErrNotFound is what every repository returns for a missing row, so callers
// can distinguish "no such thing" from "the database is unwell" without
// matching on driver errors.
var ErrNotFound = errors.New("not found")

// IsUniqueViolation reports whether an error is Postgres refusing a duplicate
// key. Here rather than in a service because pgx is deliberately fenced into
// this package: services express invariants as unique indexes and ask this one
// question about the answer.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// DB is a pool plus the migration and outbox machinery.
type DB struct {
	pool   *pgxpool.Pool
	schema string
	clk    clock.Clock
}

// Row and Rows are this package's own scanning types.
//
// They exist so that a service can read a database without naming the driver.
// depguard already forbids importing pgx outside this package; the point of
// these is to make obeying that rule the path of least resistance rather than
// an obstacle to work around.
type Row interface {
	Scan(dest ...any) error
}

// Rows is an iterator. Close is idempotent and must always be called.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Querier is what a repository is handed. Deliberately narrow: the same methods
// exist on the pool and inside a transaction, so a repository method does not
// know or care which it is running in — which is what makes "write the row and
// enqueue the outbox entry atomically" a thing you cannot forget to do.
type Querier interface {
	// Exec returns the number of rows affected, which is how an idempotent
	// insert reports whether it was the one that did the work.
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// querier wraps a pgx pool or transaction as a Querier.
type querier struct{ q pgxQuerier }

type pgxQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (w querier) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := w.q.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// The linter cannot see that the caller closes these, because the caller is on
// the other side of the Rows interface. Collect and CollectOne close for you,
// and every direct user in this repository defers a Close — which is exactly
// what the linter would be checking if it could follow the indirection.
//
//nolint:sqlclosecheck // ownership passes to the caller; Collect closes.
func (w querier) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return w.q.Query(ctx, sql, args...)
}

func (w querier) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return row{w.q.QueryRow(ctx, sql, args...)}
}

// row maps the driver's "no rows" onto ErrNotFound, so every caller can use one
// sentinel instead of each one remembering to translate.
type row struct{ r pgx.Row }

func (r row) Scan(dest ...any) error {
	err := r.r.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Collect reads every row through scan. It closes the iterator and reports the
// iteration error, which is the step most hand-written loops forget — and a
// forgotten rows.Err() is a truncated result set that looks like an empty one.
func Collect[T any](rows Rows, scan func(Row) (T, error)) ([]T, error) {
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// CollectOne reads exactly one row, returning ErrNotFound when there is none.
func CollectOne[T any](rows Rows, scan func(Row) (T, error)) (T, error) {
	var zero T
	items, err := Collect(rows, scan)
	if err != nil {
		return zero, err
	}
	if len(items) == 0 {
		return zero, ErrNotFound
	}
	return items[0], nil
}

// Open connects, waits for the database to accept queries, and sets the search
// path to the service's own schema.
//
// The wait is here rather than in the caller because every service needs it and
// a service that exits on a database that is three seconds from ready turns a
// compose start-up into a restart loop.
func Open(ctx context.Context, dsn, schema string, clk clock.Clock) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// search_path per connection, so no query has to qualify a table name and
	// no service can reach another's tables by forgetting to.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	deadline := clk.Now().Add(60 * time.Second)
	for {
		if err = pool.Ping(ctx); err == nil {
			break
		}
		if ctx.Err() != nil || clk.Now().After(deadline) {
			pool.Close()
			return nil, fmt.Errorf("database not ready after 60s: %w", err)
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return &DB{pool: pool, schema: schema, clk: clk}, nil
}

// Close releases the pool.
func (db *DB) Close() { db.pool.Close() }

// Ping is what a service's /readyz answers with. Readiness that does not touch
// the database is readiness that lies during a failover.
func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// Q returns the pool as a Querier, for reads that need no transaction.
func (db *DB) Q() Querier { return querier{db.pool} }

// Schema is the service's schema name.
func (db *DB) Schema() string { return db.schema }

// ClaimServiceNonce atomically claims a verified service-auth nonce. The
// nonce table is provisioned by Migrate in every service schema, and the
// primary key makes concurrent replicas converge on one claimant.
func (db *DB) ClaimServiceNonce(ctx context.Context, serviceID, nonce string, expiresAt time.Time) (bool, error) {
	return ClaimServiceNonce(ctx, db.Q(), serviceID, nonce, expiresAt)
}

// ClaimServiceNonce is the Querier form used by tests and transaction-owned
// callers. Expired rows are pruned on each claim, keeping retention bounded by
// the configured replay window without an unbounded background process.
func ClaimServiceNonce(ctx context.Context, q Querier, serviceID, nonce string, expiresAt time.Time) (bool, error) {
	if _, err := q.Exec(ctx, `DELETE FROM service_auth_nonces WHERE expires_at <= now()`); err != nil {
		return false, err
	}
	affected, err := q.Exec(ctx, `
		INSERT INTO service_auth_nonces (service_id, nonce, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (service_id, nonce) DO NOTHING`, serviceID, nonce, expiresAt)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// InTx runs fn in a transaction, committing if it returns nil.
//
// A failed rollback is deliberately not reported over the function's own error:
// the caller needs to know why the work failed, and "rollback failed" as the
// only message loses that.
func (db *DB) InTx(ctx context.Context, fn func(tx Querier) error) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(querier{tx}); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
