package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// Deliverer performs the side effect a message describes. It is a function
// rather than an HTTP client so that pkg/store stays a database package: the
// outbox is about atomicity, not about transport.
//
// It must be idempotent. The relay is at-least-once by construction, and a
// consumer that is not idempotent turns a redelivery into a double payment.
type Deliverer func(ctx context.Context, topic string, payload json.RawMessage) error

// Relay drains the outbox.
type Relay struct {
	db      *DB
	deliver Deliverer
	log     *slog.Logger
	clk     clock.Clock
	every   time.Duration
	batch   int
}

// NewRelay builds a relay. every is how often it looks when there is nothing to
// do; a batch that delivered work is followed immediately by another look.
func NewRelay(db *DB, deliver Deliverer, log *slog.Logger, clk clock.Clock, every time.Duration) *Relay {
	return &Relay{db: db, deliver: deliver, log: log, clk: clk, every: every, batch: 1}
}

// Run drains until the context is cancelled.
func (r *Relay) Run(ctx context.Context) {
	for {
		n, err := r.Drain(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Error("outbox drain failed", "error", err)
		}
		if n > 0 && ctx.Err() == nil {
			continue // more may be waiting; do not sleep through a backlog
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.every):
		}
	}
}

// Drain delivers one batch and returns how many it handled. Exported so the
// harness can drain deterministically instead of waiting for a ticker — a test
// that sleeps to let a background loop catch up is a test that is flaky on a
// slow runner.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	msgs, err := r.db.Claim(ctx, r.batch)
	if err != nil {
		return 0, err
	}
	for _, m := range msgs {
		deliveryCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		deliveryErr := r.deliver(deliveryCtx, m.Topic, m.Payload)
		cancel()
		if err := deliveryErr; err != nil {
			r.log.Warn("outbox delivery failed, will retry",
				"topic", m.Topic, "id", m.ID, "attempts", m.Attempts, "error", err)
			if err := r.db.Failed(ctx, m.ID, m.Attempts, err.Error()); err != nil {
				return len(msgs), err
			}
			continue
		}
		if err := r.db.Delivered(ctx, m.ID, m.Attempts); err != nil {
			return len(msgs), err
		}
	}
	return len(msgs), nil
}
