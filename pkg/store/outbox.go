package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The transactional outbox.
//
// The thing it exists to prevent: a claim is accepted, the row is committed,
// and the call that releases the payment fails. The worker's record says the
// work counted and no money moved, and nothing anywhere records that a call was
// owed. W4 says every T=7 exit releases payment; an in-process HTTP call after
// a commit cannot promise that, and a message written in the same transaction
// as the state change can.
//
// The relay is at-least-once, so every consumer has to be idempotent. That is
// stated here because it is a property of the design, not an accident: the
// alternative is at-most-once, which loses payments.

// OutboxMessage is one pending side effect.
type OutboxMessage struct {
	ID        int64
	Topic     string
	Payload   json.RawMessage
	Attempts  int
	CreatedAt time.Time
}

// Enqueue writes a message in the caller's transaction. It takes a Querier
// rather than the DB precisely so it cannot be called outside one.
func Enqueue(ctx context.Context, tx Querier, topic string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO outbox (topic, payload) VALUES ($1, $2)`, topic, raw)
	if err != nil {
		return fmt.Errorf("enqueue %s: %w", topic, err)
	}
	return nil
}

// Claim takes up to n undelivered messages and marks them in flight.
//
// FOR UPDATE SKIP LOCKED is what lets two relays run without either waiting on
// the other or both taking the same row.
func (db *DB) Claim(ctx context.Context, n int) ([]OutboxMessage, error) {
	rows, err := db.pool.Query(ctx, `
		UPDATE outbox SET attempts = attempts + 1, claimed_at = now()
		WHERE id IN (
			SELECT id FROM outbox
			WHERE delivered_at IS NULL AND (next_attempt_at IS NULL OR next_attempt_at <= now())
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, topic, payload, attempts, created_at`, n)
	if err != nil {
		return nil, fmt.Errorf("claim outbox: %w", err)
	}
	defer rows.Close()

	msgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OutboxMessage, error) {
		var m OutboxMessage
		return m, r.Scan(&m.ID, &m.Topic, &m.Payload, &m.Attempts, &m.CreatedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("scan outbox: %w", err)
	}
	return msgs, nil
}

// Delivered marks a message done.
func (db *DB) Delivered(ctx context.Context, id int64) error {
	_, err := db.pool.Exec(ctx, `UPDATE outbox SET delivered_at = now() WHERE id = $1`, id)
	return err
}

// Failed schedules a retry with a bounded backoff.
//
// It never drops a message. A message that has failed twenty times is a message
// someone has to look at — and a queue that quietly discards a payment release
// is the failure this whole mechanism exists to prevent (W10: every held
// payment has a reason with an owner).
func (db *DB) Failed(ctx context.Context, id int64, cause string) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE outbox
		SET last_error = $2,
		    next_attempt_at = now() + LEAST(interval '5 minutes', attempts * interval '2 seconds')
		WHERE id = $1`, id, cause)
	return err
}

// Pending counts messages still owed. The harness asserts on this, and an
// operator should be able to alert on it.
func (db *DB) Pending(ctx context.Context) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE delivered_at IS NULL`).Scan(&n)
	return n, err
}
