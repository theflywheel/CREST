package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/store"
)

// A mechanism activation is allowed to clear the disbursement gate only. This
// uses the real payment schema and outbox so a later rate publication or an
// evidence read outage cannot silently change an already-priced obligation.
func TestMechanismActivationPreservesPricedHoldInDatabase(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	name := fmt.Sprintf("payments_activation_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, name, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Q().Exec(ctx, `DROP SCHEMA "`+strings.ReplaceAll(name, `"`, `""`)+`" CASCADE`)
		db.Close()
	})
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	pricingAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mechanism := Mechanism{
		ID: "mechanism-activation-1", ContextID: "context-activation", OwnerPartyID: "party-funder",
		State: mechanismConfigured, Config: map[string]any{}, CreatedByPartyID: "party-funder", CreatedAt: pricingAt,
	}
	in := Instruction{
		ID: "instruction-activation-1", ClaimID: "claim-activation-1", UnitID: "unit-activation-1",
		PartyID: "party-worker", ContextID: mechanism.ContextID, AmountMinor: 250,
		Currency: "KES", RateRecordID: "rate-september", RateVersion: 2,
		PricingAt: &pricingAt, ReleasedBy: "auto", ReleasedAt: pricingAt.Add(7 * 24 * time.Hour),
		State: "HELD", Held: &HeldReason{Code: "mechanism_not_live", Explanation: "mechanism is configured but not active", OwnerPartyID: "party-funder"},
		CreatedAt: pricingAt,
	}
	if err := db.InTx(ctx, func(tx store.Querier) error {
		createdMechanism, err := insertMechanism(ctx, tx, mechanism)
		if err != nil {
			return err
		}
		if !createdMechanism {
			return fmt.Errorf("activation mechanism was not inserted")
		}
		for _, rec := range []mechanismRecord{
			{ID: "record-reconciliation", MechanismID: mechanism.ID, Kind: recordReconciliationAgreement, ActorPartyID: "party-funder", At: pricingAt},
			{ID: "record-batching", MechanismID: mechanism.ID, Kind: recordBatchingChoice, ActorPartyID: "party-funder", At: pricingAt},
			{ID: "record-qualification-submitted", MechanismID: mechanism.ID, Kind: recordQualificationSubmitted, ActorPartyID: "party-funder", At: pricingAt},
			{ID: "record-qualification-verified", MechanismID: mechanism.ID, Kind: recordQualificationVerified, ActorPartyID: "party-funder", At: pricingAt},
		} {
			if err := insertMechanismRecord(ctx, tx, rec); err != nil {
				return err
			}
		}
		if err := insertTestDisbursement(ctx, tx, testDisbursement{
			ID: "test-disbursement-activation", MechanismID: mechanism.ID, RequestedBy: "party-funder",
			AmountMinor: 1, Currency: "KES", Destination: "sandbox", State: "SUCCEEDED", At: pricingAt,
		}); err != nil {
			return err
		}
		created, err := insertInstruction(ctx, tx, in)
		if err != nil {
			return err
		}
		if !created {
			return fmt.Errorf("priced hold was not inserted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var activated Mechanism
	var released []string
	if err := db.InTx(ctx, func(tx store.Querier) error {
		var conds []activationCondition
		var err error
		activated, conds, released, err = activateMechanismAndRelease(ctx, tx, mechanism.ID, "party-funder", pricingAt.Add(8*24*time.Hour))
		if err != nil {
			return err
		}
		if len(conds) != 4 {
			return fmt.Errorf("activation conditions = %d, want 4", len(conds))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if activated.State != mechanismActive || len(released) != 1 || released[0] != in.ID {
		t.Fatalf("activation result = state %q released %v, want ACTIVE and [%s]", activated.State, released, in.ID)
	}

	got, err := getInstructionByID(ctx, db.Q(), in.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "RELEASED" || got.Held != nil || got.AmountMinor != in.AmountMinor || got.Currency != in.Currency ||
		got.RateRecordID != in.RateRecordID || got.RateVersion != in.RateVersion || got.PricingAt == nil || !got.PricingAt.Equal(pricingAt) {
		t.Fatalf("activation changed the priced obligation: got %+v, want amount/rate/snapshot from %+v", got, in)
	}
	messages, err := db.Claim(ctx, 1)
	if err != nil || len(messages) != 1 {
		t.Fatalf("activation outbox messages = %d, err=%v", len(messages), err)
	}
	var queued Instruction
	if err := json.Unmarshal(messages[0].Payload, &queued); err != nil {
		t.Fatalf("decode rail payload: %v", err)
	}
	if queued.RateRecordID != in.RateRecordID || queued.RateVersion != in.RateVersion || queued.PricingAt == nil || !queued.PricingAt.Equal(pricingAt) {
		t.Fatalf("rail payload lost the immutable rate link: %+v", queued)
	}
}
