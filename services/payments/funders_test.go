// Pure tests for the funders wave's decisions. Named by situation, driven by
// values — no clock, no database, no HTTP.
package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

var t0 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// ─── F-1: rate ownership ────────────────────────────────────────────────────

func TestNobodyAssignedMeansNobodyAuthors(t *testing.T) {
	if err := rateAuthoring(nil, "did:crest:party:nadia"); !errors.Is(err, errNoRateOwner) {
		t.Fatalf("authoring with no assignment: got %v, want errNoRateOwner", err)
	}
}

func TestOnlyTheAssignedOwnerAuthors(t *testing.T) {
	current := &RateOwnerAssignment{AssigneePartyID: "did:crest:party:nadia"}
	if err := rateAuthoring(current, "did:crest:party:somebody-else"); !errors.Is(err, errNotRateOwner) {
		t.Fatalf("authoring by a non-owner: got %v, want errNotRateOwner", err)
	}
	if err := rateAuthoring(current, ""); !errors.Is(err, errNotRateOwner) {
		t.Fatalf("authoring by nobody: got %v, want errNotRateOwner", err)
	}
	if err := rateAuthoring(current, "did:crest:party:nadia"); err != nil {
		t.Fatalf("the assigned owner authoring: got %v, want nil", err)
	}
}

func TestAnAssignmentRecordsBothNames(t *testing.T) {
	if err := validAssignment("", "did:crest:party:peter"); err == nil {
		t.Fatal("an assignment with no assignee was accepted")
	}
	if err := validAssignment("did:crest:party:nadia", ""); err == nil {
		t.Fatal("an assignment with no assigner was accepted; nobody is on the record")
	}
	if err := validAssignment("did:crest:party:nadia", "did:crest:party:peter"); err != nil {
		t.Fatalf("a complete assignment was refused: %v", err)
	}
}

// ─── F-1: rates as versioned terms ──────────────────────────────────────────

func rate(version, amount int, effective time.Time) rateVersion {
	return rateVersion{Version: version, Payload: schema.PaymentSetupLinkedRecordPayload{
		RatePerOutcomeUnit: schema.PaymentSetupLinkedRecordPayloadRatePerOutcomeUnit{
			AmountMinor: amount, Currency: "KES",
		},
		EffectiveFrom: effective,
	}}
}

func TestANewRateIsANewVersionNeverARewrite(t *testing.T) {
	if v := nextRateVersion(nil); v != 1 {
		t.Fatalf("first version = %d, want 1", v)
	}
	existing := []rateVersion{rate(1, 100, t0), rate(3, 200, t0)}
	if v := nextRateVersion(existing); v != 4 {
		t.Fatalf("next version = %d, want 4 (one past the highest that exists)", v)
	}
}

func TestPaymentsReadTheVersionInForceAtTheWorkPeriodStart(t *testing.T) {
	versions := []rateVersion{
		rate(1, 15000, t0),                  // in force from 1 Sep
		rate(2, 20000, t0.AddDate(0, 1, 0)), // a raise from 1 Oct
	}
	sept := t0.AddDate(0, 0, 10)
	got, ok := rateInForceAt(versions, sept)
	if !ok || got.Version != 1 {
		t.Fatalf("a September release priced by version %d (ok=%v), want 1: work done is not repriced by a later publication", got.Version, ok)
	}
	oct := t0.AddDate(0, 1, 5)
	got, ok = rateInForceAt(versions, oct)
	if !ok || got.Version != 2 {
		t.Fatalf("an October release priced by version %d (ok=%v), want 2", got.Version, ok)
	}
}

func TestARatePublishedForTheFutureIsNotYetInForce(t *testing.T) {
	versions := []rateVersion{rate(1, 15000, t0.AddDate(0, 2, 0))}
	if _, ok := rateInForceAt(versions, t0); ok {
		t.Fatal("a rate effective only in the future priced a payment today")
	}
}

// ─── F-2: the activation gate ───────────────────────────────────────────────

func allFacts() mechanismFacts {
	at := t0
	return mechanismFacts{
		TestSucceededAt: &at, ReconciliationAgreedAt: &at,
		BatchingChosenAt: &at, QualificationVerified: &at,
	}
}

func configured() Mechanism {
	return Mechanism{ID: "mechanism:1", ContextID: "context:riverside",
		OwnerPartyID: "did:crest:party:daniel", State: mechanismConfigured}
}

func TestEveryUnmetConditionRefusesActivation(t *testing.T) {
	clear := []func(*mechanismFacts){
		func(f *mechanismFacts) { f.TestSucceededAt = nil },
		func(f *mechanismFacts) { f.ReconciliationAgreedAt = nil },
		func(f *mechanismFacts) { f.BatchingChosenAt = nil },
		func(f *mechanismFacts) { f.QualificationVerified = nil },
	}
	for i, c := range clear {
		f := allFacts()
		c(&f)
		_, conds, err := activateMechanism(configured(), f, "did:crest:party:daniel", t0)
		if !errors.Is(err, errMechanismGatesUnmet) {
			t.Fatalf("condition %d unmet, activation gave %v, want errMechanismGatesUnmet", i, err)
		}
		unmet := 0
		for _, cond := range conds {
			if !cond.Satisfied {
				unmet++
			}
		}
		if unmet != 1 {
			t.Fatalf("condition %d: the refusal listed %d unmet conditions, want exactly the 1 that is", i, unmet)
		}
	}
}

func TestActivationIsAnActWithAnActor(t *testing.T) {
	if _, _, err := activateMechanism(configured(), allFacts(), "", t0); err == nil {
		t.Fatal("a mechanism went live with nobody's name on the activation")
	}
}

func TestAllConditionsMetActivates(t *testing.T) {
	m, conds, err := activateMechanism(configured(), allFacts(), "did:crest:party:daniel", t0)
	if err != nil {
		t.Fatalf("activation refused with every condition met: %v", err)
	}
	if m.State != mechanismActive || m.ActivatedAt == nil || m.ActivatedBy == nil ||
		*m.ActivatedBy != "did:crest:party:daniel" {
		t.Fatalf("activation did not record the act: %+v", m)
	}
	for _, c := range conds {
		if !c.Satisfied {
			t.Fatalf("condition %q reported unsatisfied on a successful activation", c.Name)
		}
	}
}

func TestActivatingALiveMechanismIsIdempotent(t *testing.T) {
	live := configured()
	live.State = mechanismActive
	// Idempotent even with the facts unreadable: live is the outcome asked for.
	m, _, err := activateMechanism(live, mechanismFacts{}, "did:crest:party:daniel", t0)
	if err != nil || m.State != mechanismActive {
		t.Fatalf("re-activating a live mechanism: %v, state %s", err, m.State)
	}
}

func TestHalfDoneIsARealState(t *testing.T) {
	if s := standing(configured()); s != "configured-not-live" {
		t.Fatalf("a configured, not-live mechanism stands as %q, want configured-not-live", s)
	}
	live := configured()
	live.State = mechanismActive
	if s := standing(live); s != "live" {
		t.Fatalf("a live mechanism stands as %q", s)
	}
}

// ─── F-2: the gate sits in front of disbursement (f2_9) ─────────────────────

func TestNoMechanismMeansNoGate(t *testing.T) {
	if hold := holdForMechanism(nil); hold != nil {
		t.Fatalf("a context with no mechanism record was gated: %+v", hold)
	}
}

func TestALiveMechanismDisburses(t *testing.T) {
	live := configured()
	live.State = mechanismActive
	if hold := holdForMechanism(&live); hold != nil {
		t.Fatalf("a live mechanism held a payment: %+v", hold)
	}
}

func TestANotLiveMechanismHoldsWithAReasonAndAnOwner(t *testing.T) {
	m := configured()
	hold := holdForMechanism(&m)
	if hold == nil {
		t.Fatal("a not-live mechanism let disbursement through the gate")
	}
	// W10, asserted piece by piece: the code a machine can filter on, the
	// sentence a person can act on, and the named owner it lands with.
	if hold.Code != "mechanism_not_live" {
		t.Fatalf("hold code %q, want mechanism_not_live", hold.Code)
	}
	if strings.TrimSpace(hold.Explanation) == "" {
		t.Fatal("the hold carries no explanation; a worker would see a missing payment and silence")
	}
	if hold.OwnerPartyID != m.OwnerPartyID {
		t.Fatalf("the hold is owned by %q, want the mechanism owner %q — a reason with no owner is a dead end",
			hold.OwnerPartyID, m.OwnerPartyID)
	}
}

// ─── F-2: facts come from acts, not assertions ──────────────────────────────

func TestAFailedTestSatisfiesNothing(t *testing.T) {
	f := factsFor(nil, []testDisbursement{{State: "FAILED", At: t0}})
	if f.TestSucceededAt != nil {
		t.Fatal("a failed test disbursement satisfied the proving condition")
	}
	f = factsFor(nil, []testDisbursement{{State: "FAILED", At: t0}, {State: "SUCCEEDED", At: t0.Add(time.Hour)}})
	if f.TestSucceededAt == nil {
		t.Fatal("a succeeded test after a failed one did not satisfy the condition")
	}
}

func TestFactsReadTheRecordedActs(t *testing.T) {
	records := []mechanismRecord{
		{Kind: recordReconciliationAgreement, At: t0},
		{Kind: recordBatchingChoice, At: t0.Add(time.Hour)},
		{Kind: recordQualificationSubmitted, At: t0.Add(2 * time.Hour)},
		{Kind: recordQualificationVerified, At: t0.Add(3 * time.Hour)},
		{Kind: recordStatementAgreement, At: t0.Add(4 * time.Hour)},
	}
	f := factsFor(records, nil)
	if f.ReconciliationAgreedAt == nil || f.BatchingChosenAt == nil ||
		f.QualificationSubmitted == nil || f.QualificationVerified == nil {
		t.Fatalf("recorded acts did not become facts: %+v", f)
	}
	if f.TestSucceededAt != nil {
		t.Fatal("a test-success fact appeared with no test on record")
	}
}

// ─── F-2: the batching choice and the statement's limits ────────────────────

func TestABatchingChoiceRecordsItsTradeoff(t *testing.T) {
	if err := validBatchingChoice("daily-23:00", ""); err == nil {
		t.Fatal("a batching choice with the worker's cost unstated was accepted")
	}
	if err := validBatchingChoice("", "workers wait up to a day"); err == nil {
		t.Fatal("a batching choice choosing nothing was accepted")
	}
	if err := validBatchingChoice("daily-23:00", "workers wait up to a day for the evening batch"); err != nil {
		t.Fatalf("a complete batching choice was refused: %v", err)
	}
}

func TestAStatementStatesItsLimits(t *testing.T) {
	limits := statementLimits()
	if len(limits) == 0 {
		t.Fatal("a statement with no stated limits claims an authority it does not have")
	}
	joined := strings.Join(limits, " ")
	if !strings.Contains(joined, "advisory") {
		t.Fatal("the limits never say the statement is advisory")
	}
}
