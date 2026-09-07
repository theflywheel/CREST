package store

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestOutboxStatsSeparatesNewAndRetrying(t *testing.T) {
	db := databaseForTest(t)
	ctx := context.Background()
	fs := fstest.MapFS{"migrations/0001.sql": {Data: []byte(`CREATE TABLE outbox (
 id bigserial PRIMARY KEY, topic text NOT NULL, payload jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now(),
 claimed_at timestamptz, next_attempt_at timestamptz, delivered_at timestamptz,
 last_error text);`)}}
	if err := db.Migrate(ctx, fs, "migrations"); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(q Querier) error {
		return Enqueue(ctx, q, "one", map[string]string{"value": "not exported"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(q Querier) error {
		return Enqueue(ctx, q, "two", map[string]string{"value": "not exported"})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE outbox SET attempts = 1 WHERE topic = 'two'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE outbox SET delivered_at = now() WHERE topic = 'one'"); err != nil {
		t.Fatal(err)
	}
	got, err := db.OutboxStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending != 0 || got.Retrying != 1 || got.OldestAgeSecs < 0 {
		t.Fatalf("stats = %+v, want one retrying message and non-negative age", got)
	}
}
