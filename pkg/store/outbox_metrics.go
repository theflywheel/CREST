package store

import (
	"context"
	"fmt"
)

// OutboxStats is the bounded operational view of a service's durable work.
// Pending messages have never been claimed; retrying messages have been
// claimed at least once and remain undelivered. Payloads and topics are never
// exposed by this view.
type OutboxStats struct {
	Pending       int64
	Retrying      int64
	OldestAgeSecs float64
}

// OutboxStats returns counts and the age of the oldest undelivered message.
// The caller supplies a timeout because an observability endpoint must not
// hold a database connection indefinitely or turn a database outage into an
// apparently healthy empty result.
func (db *DB) OutboxStats(ctx context.Context) (OutboxStats, error) {
	var stats OutboxStats
	err := db.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE delivered_at IS NULL AND attempts = 0),
			count(*) FILTER (WHERE delivered_at IS NULL AND attempts > 0),
			COALESCE(EXTRACT(EPOCH FROM (now() - min(created_at) FILTER (WHERE delivered_at IS NULL))), 0)
		FROM outbox WHERE delivered_at IS NULL`).Scan(&stats.Pending, &stats.Retrying, &stats.OldestAgeSecs)
	if err != nil {
		return OutboxStats{}, fmt.Errorf("read outbox stats: %w", err)
	}
	return stats, nil
}
