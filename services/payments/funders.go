// The funders wave (F-1, F-2) as pure decisions: who owns a rate, which rate
// version is in force, what stands between a mechanism and ACTIVE, and what a
// not-yet-live mechanism does to a payment.
//
// The rule this file exists to keep exact (f2_9): the qualification and
// activation gate sits in front of DISBURSEMENT only. A confirmation-window
// exit NEVER fails to create the payment obligation — all four exits release
// (W4) — and what a not-live mechanism produces is a HELD instruction carrying
// a reason and a named owner (W10), never a missing one.
package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// ─── F-1: rate ownership and rates as terms ─────────────────────────────────

// RateOwnerAssignment is f1_2's record: who may author rates for a
// definition, who put them there, and when. Superseded assignments survive as
// history; only one is current.
type RateOwnerAssignment struct {
	ID                string     `json:"id"`
	DefinitionID      string     `json:"definitionId"`
	AssigneePartyID   string     `json:"assigneePartyId"`
	AssignedByPartyID string     `json:"assignedByPartyId"`
	AssignedAt        time.Time  `json:"assignedAt"`
	SupersededAt      *time.Time `json:"supersededAt,omitempty"`
}

var (
	errNoRateOwner  = errors.New("no rate owner is assigned to this definition; anyone can ask, only an assignment answers")
	errNotRateOwner = errors.New("only the assigned rate owner authors rates for this definition")
)

// validAssignment refuses the assignments that record nothing.
func validAssignment(assignee, assigner string) error {
	if strings.TrimSpace(assignee) == "" {
		return errors.New("an assignment names who will own the rate")
	}
	if strings.TrimSpace(assigner) == "" {
		return errors.New("an assignment records who assigned it; an ownerless assignment is not one")
	}
	return nil
}

// rateAuthoring decides whether author may publish a rate for a definition,
// given the current assignment. The author prices a unit somebody else
// defined (f1_3); nothing on this path can touch the definition — the rate is
// a LinkedRecord keyed to it, and this service never writes definitions.
func rateAuthoring(current *RateOwnerAssignment, author string) error {
	if current == nil {
		return errNoRateOwner
	}
	if author == "" || author != current.AssigneePartyID {
		return errNotRateOwner
	}
	return nil
}

// rateVersion is one published rate, read back from the definition's
// payment-setup LinkedRecords. A rate is terms, not a setting (f1_4): each
// publication is a new LinkedRecord version naming what it supersedes, and no
// code path in this repository edits one.
type rateVersion struct {
	ID      string
	Version int
	Payload schema.PaymentSetupLinkedRecordPayload
}

// nextRateVersion is the version a new publication takes: one past the
// highest that exists, never a rewrite of one that does.
func nextRateVersion(existing []rateVersion) int {
	max := 0
	for _, r := range existing {
		if r.Version > max {
			max = r.Version
		}
	}
	return max + 1
}

// rateInForceAt picks the version in force at a moment: among versions whose
// effectiveFrom is not after `at`, the highest version wins. Payments pass the
// work period start as `at`, so a rate change after the work never reprices it,
// in either direction.
//
// The bool is false when versions exist but none is yet effective; that is
// not the same as no rate at all, and the caller words the hold differently.
func rateInForceAt(versions []rateVersion, at time.Time) (rateVersion, bool) {
	var best rateVersion
	found := false
	for _, v := range versions {
		if v.Payload.EffectiveFrom.After(at) {
			continue
		}
		if !found || v.Version > best.Version {
			best, found = v, true
		}
	}
	return best, found
}

// ─── F-2: the mechanism, its gates, and the disbursement hold ───────────────

// Mechanism is the payment mechanism for one context. CONFIGURED is f1_5's
// half-done state — rates can exist, the record exists, and no money can move
// — and it is a real recorded state, not an error.
type Mechanism struct {
	ID               string         `json:"id"`
	ContextID        string         `json:"contextId"`
	OwnerPartyID     string         `json:"ownerPartyId"`
	State            string         `json:"state"`
	Config           map[string]any `json:"config,omitempty"`
	CreatedByPartyID string         `json:"createdByPartyId"`
	CreatedAt        time.Time      `json:"createdAt"`
	ActivatedAt      *time.Time     `json:"activatedAt,omitempty"`
	ActivatedBy      *string        `json:"activatedBy,omitempty"`
}

const (
	mechanismConfigured = "CONFIGURED"
	mechanismActive     = "ACTIVE"
)

// The recorded acts of setting a mechanism up (mechanism_records.kind).
const (
	recordRailsChosen             = "rails-chosen"
	recordProviderConnected       = "provider-connected"
	recordReconciliationAgreement = "reconciliation-agreement"
	recordStatementAgreement      = "statement-agreement"
	recordBatchingChoice          = "batching-choice"
	recordQualificationSubmitted  = "qualification-submitted"
	recordQualificationVerified   = "qualification-verified"
)

// mechanismFacts is what has actually been done, read from the records and
// the test-disbursement table. Conditions are satisfied by acts, never by a
// caller asserting them.
type mechanismFacts struct {
	TestSucceededAt        *time.Time
	ReconciliationAgreedAt *time.Time
	BatchingChosenAt       *time.Time
	QualificationVerified  *time.Time
	QualificationSubmitted *time.Time
}

var errMechanismGatesUnmet = errors.New("the mechanism's activation conditions are not all satisfied")

// activationCondition is one readable answer to "what does this mechanism
// still need before real money moves?" — the same shape project activation
// uses (#173), restated here because payments is a separate deployable.
type activationCondition struct {
	Name        string     `json:"name"`
	Satisfied   bool       `json:"satisfied"`
	SatisfiedAt *time.Time `json:"satisfiedAt,omitempty"`
	Because     string     `json:"because,omitempty"`
}

// mechanismConditions lists everything standing between CONFIGURED and
// ACTIVE, satisfied ones included — the same posture as project activation
// (#173): a list of only unmet conditions cannot tell a ready mechanism from
// one whose gates were never real.
//
// The four are L1 by the layering test: no two deployments could reasonably
// disagree that real money needs the path proven once, a findable-mismatch
// file agreed, the batching trade-off chosen by a named person, and the
// payer's authority verified. What each of those means concretely — the
// amount of the test, the file's cadence, the batching window — stays L2 and
// lives in the recorded payloads.
func mechanismConditions(f mechanismFacts) []activationCondition {
	return []activationCondition{
		{
			Name: "test-disbursement-succeeded", Satisfied: f.TestSucceededAt != nil,
			SatisfiedAt: f.TestSucceededAt,
			Because:     "one real payment has been proven through the configured mechanism, end to end, with its result recorded (f2_4)",
		},
		{
			Name: "reconciliation-file-agreed", Satisfied: f.ReconciliationAgreedAt != nil,
			SatisfiedAt: f.ReconciliationAgreedAt,
			Because:     "the export where every line ties to a payment instruction is agreed, so a mismatch is findable (f2_5, W10)",
		},
		{
			Name: "batching-choice-recorded", Satisfied: f.BatchingChosenAt != nil,
			SatisfiedAt: f.BatchingChosenAt,
			Because:     "batching is paid for by the worker in waiting time; the timing choice carries who chose and when (f2_7)",
		},
		{
			Name: "qualification-verified", Satisfied: f.QualificationVerified != nil,
			SatisfiedAt: f.QualificationVerified,
			Because:     "the organisation's authority to move real money was verified before any real money moves (f2_9, f2_10)",
		},
	}
}

// activateMechanism moves CONFIGURED to ACTIVE, or refuses with the
// conditions readable. Idempotent on an already-ACTIVE mechanism: live is the
// outcome the caller wanted.
func activateMechanism(m Mechanism, f mechanismFacts, by string, at time.Time) (Mechanism, []activationCondition, error) {
	conds := mechanismConditions(f)
	if m.State == mechanismActive {
		return m, conds, nil
	}
	if strings.TrimSpace(by) == "" {
		return m, conds, errors.New("activation is an act with an actor; going live with nobody's name on it is not recorded, it is assumed")
	}
	for _, c := range conds {
		if !c.Satisfied {
			return m, conds, errMechanismGatesUnmet
		}
	}
	m.State = mechanismActive
	when := at
	m.ActivatedAt = &when
	m.ActivatedBy = &by
	return m, conds, nil
}

// standing is f1_5 made readable: the half-done state, named rather than
// implied. It is derived, never stored — the same rule as trust tiers.
func standing(m Mechanism) string {
	if m.State == mechanismActive {
		return "live"
	}
	return "configured-not-live"
}

// holdForMechanism is f2_9's whole decision, in one function so it cannot be
// half-applied: given the mechanism governing an instruction's context (nil
// when the context has none), does disbursement proceed or hold?
//
// nil, nil  → disburse. Either the mechanism is ACTIVE, or no mechanism
// record governs this context at all — deployments from before this surface
// existed are not retroactively gated (their instructions carry no context
// either).
//
// A non-nil reason ALWAYS carries an owner: the mechanism's owner, the named
// person a worker's "where is my money?" reaches (W10). What it never does is
// stop the instruction being created — the caller creates it HELD.
func holdForMechanism(m *Mechanism) *HeldReason {
	if m == nil || m.State == mechanismActive {
		return nil
	}
	return &HeldReason{
		Code: "mechanism_not_live",
		Explanation: "this payment is owed and recorded, and it will be sent when the payment mechanism goes live; " +
			"the mechanism is configured but has not passed its activation gate",
		OwnerPartyID: m.OwnerPartyID,
	}
}

// validBatchingChoice refuses a choice that records no chooser or hides the
// trade-off. The window value itself is L2 and unread.
func validBatchingChoice(window, tradeoff string) error {
	if strings.TrimSpace(window) == "" {
		return errors.New("a batching choice names the timing chosen")
	}
	if strings.TrimSpace(tradeoff) == "" {
		return errors.New("batching is paid for by the worker; the choice records the trade-off in a sentence, not silently")
	}
	if len(tradeoff) > 2000 {
		return fmt.Errorf("a trade-off of %d bytes is a document, not a sentence", len(tradeoff))
	}
	return nil
}

// statementLimits is f2_6's honest limit, stated on every statement rather
// than in a footnote somewhere. The statement is advisory: CREST never holds
// or moves money (§10), so the rail's own statement is the authoritative one.
func statementLimits() []string {
	return []string{
		"advisory only: CREST never holds or moves money, and the rail's own statement is the authoritative record of what was paid",
		"covers payment instructions and rail confirmations recorded by this deployment, up to the moment it was generated",
		"a held payment appears here with its reason and owner; it is not missing, it is explained",
	}
}
