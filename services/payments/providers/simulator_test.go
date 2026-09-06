package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

func TestSimulatorIsDurableAndDoesNotSettleOnSubmit(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	schemaName := fmt.Sprintf("provider_contract_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Q().Exec(ctx, `DROP SCHEMA "`+strings.ReplaceAll(schemaName, `"`, `""`)+`" CASCADE`)
		db.Close()
	})
	if err := db.Migrate(ctx, fstest.MapFS{"migrations/0001.sql": {Data: []byte(`
		CREATE TABLE payment_simulator_transfers (
		 idempotency_key text PRIMARY KEY, instruction_id text NOT NULL, context_id text NOT NULL,
		 reference text NOT NULL, amount_minor bigint NOT NULL, currency text NOT NULL,
		 destination text NOT NULL, state text NOT NULL, settled_amount_minor bigint,
			 settled_currency text, settlement_reference text, created_at timestamptz NOT NULL);`)}}, "migrations"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)
	sim := NewSimulator(db.Q(), func() time.Time { return now })
	req := Request{IdempotencyKey: "instruction-1", InstructionID: "instruction-1", ContextID: "context-1", Reference: "claim-1", AmountMinor: 125, Currency: "KES", Destination: "worker-1"}
	first, err := sim.Submit(ctx, req)
	if err != nil || first.Status != Pending {
		t.Fatalf("first simulator submit = %+v, err=%v; submit must remain pending", first, err)
	}
	replay, err := sim.Submit(ctx, req)
	if err != nil || replay.Status != Pending {
		t.Fatalf("idempotent simulator replay = %+v, err=%v", replay, err)
	}
	if _, err := sim.Submit(ctx, Request{IdempotencyKey: req.IdempotencyKey, InstructionID: req.InstructionID, ContextID: req.ContextID, Reference: req.Reference, AmountMinor: 126, Currency: req.Currency, Destination: req.Destination}); err == nil {
		t.Fatal("simulator accepted an idempotency key reused for a different amount")
	}
	if _, err := sim.Settle(ctx, "other-context", req.IdempotencyKey, "rail-1", req.AmountMinor, req.Currency); err == nil {
		t.Fatal("simulator settlement crossed a project context")
	}
	settled, err := sim.Settle(ctx, req.ContextID, req.IdempotencyKey, "rail-1", req.AmountMinor, req.Currency)
	if err != nil || settled.Status != Confirmed || settled.Reference != "rail-1" || settled.SettledAmountMinor == nil || *settled.SettledAmountMinor != req.AmountMinor {
		t.Fatalf("explicit simulator settlement = %+v, err=%v", settled, err)
	}
	replayedSettlement, err := sim.Settle(ctx, req.ContextID, req.IdempotencyKey, "rail-1", req.AmountMinor, req.Currency)
	if err != nil || replayedSettlement.Status != Confirmed || replayedSettlement.Reference != settled.Reference {
		t.Fatalf("idempotent simulator settlement replay = %+v, err=%v", replayedSettlement, err)
	}
	if replay, err := sim.Submit(ctx, req); err != nil || replay.Status != Confirmed {
		t.Fatalf("post-settlement replay = %+v, err=%v", replay, err)
	}
}
