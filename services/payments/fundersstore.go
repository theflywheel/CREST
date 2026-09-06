package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

// ─── rate owner assignments ─────────────────────────────────────────────────

// assignRateOwner supersedes any current assignment and records the new one,
// in one transaction: there is never a moment with two current owners, and
// never a gap where the old one has gone and nobody holds it.
func assignRateOwner(ctx context.Context, tx store.Querier, a RateOwnerAssignment) error {
	if _, err := tx.Exec(ctx, `
		UPDATE rate_owner_assignments SET superseded_at = $2
		WHERE definition_id = $1 AND superseded_at IS NULL`,
		a.DefinitionID, a.AssignedAt); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO rate_owner_assignments
			(id, definition_id, assignee_party_id, assigned_by_party_id, assigned_at)
		VALUES ($1,$2,$3,$4,$5)`,
		a.ID, a.DefinitionID, a.AssigneePartyID, a.AssignedByPartyID, a.AssignedAt)
	return err
}

func scanAssignments(rows store.Rows) ([]RateOwnerAssignment, error) {
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (RateOwnerAssignment, error) {
		var a RateOwnerAssignment
		err := r.Scan(&a.ID, &a.DefinitionID, &a.AssigneePartyID,
			&a.AssignedByPartyID, &a.AssignedAt, &a.SupersededAt)
		return a, err
	})
}

// rateOwnerHistory is every assignment for a definition, newest first; the
// current one, if any, is the row with no supersededAt.
func rateOwnerHistory(ctx context.Context, q store.Querier, definitionID string) ([]RateOwnerAssignment, error) {
	rows, err := q.Query(ctx, `
		SELECT id, definition_id, assignee_party_id, assigned_by_party_id,
		       assigned_at, superseded_at
		FROM rate_owner_assignments WHERE definition_id = $1
		ORDER BY assigned_at DESC, id DESC`, definitionID)
	if err != nil {
		return nil, err
	}
	return scanAssignments(rows)
}

func currentRateOwner(history []RateOwnerAssignment) *RateOwnerAssignment {
	for i := range history {
		if history[i].SupersededAt == nil {
			return &history[i]
		}
	}
	return nil
}

// rateOwnershipsFor is the definitions a party currently owns the rate for —
// the "am I a rate owner?" a console asks to decide it is looking at a rate
// owner, derived from the assignment records rather than a stored role.
func rateOwnershipsFor(ctx context.Context, q store.Querier, partyID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT definition_id FROM rate_owner_assignments
		WHERE assignee_party_id = $1 AND superseded_at IS NULL
		ORDER BY definition_id`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
}

// mechanismsOwnedBy is the mechanisms a party owns — the "am I a mechanism
// owner?" companion to rateOwnershipsFor, derived from the mechanism's own
// owner column, never a stored role.
func mechanismsOwnedBy(ctx context.Context, q store.Querier, partyID string) ([]Mechanism, error) {
	rows, err := q.Query(ctx,
		`SELECT `+mechanismColumns+` FROM mechanisms WHERE owner_party_id = $1 ORDER BY created_at`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanMechanism)
}

// ─── mechanisms ─────────────────────────────────────────────────────────────

func insertMechanism(ctx context.Context, tx store.Querier, m Mechanism) (bool, error) {
	cfg, err := json.Marshal(m.Config)
	if err != nil {
		return false, err
	}
	affected, err := tx.Exec(ctx, `
		INSERT INTO mechanisms (id, context_id, owner_party_id, state, config,
		                        created_by_party_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (context_id) DO NOTHING`,
		m.ID, m.ContextID, m.OwnerPartyID, m.State, cfg, m.CreatedByPartyID, m.CreatedAt)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func scanMechanism(r store.Row) (Mechanism, error) {
	var m Mechanism
	var cfg []byte
	if err := r.Scan(&m.ID, &m.ContextID, &m.OwnerPartyID, &m.State, &cfg,
		&m.CreatedByPartyID, &m.CreatedAt, &m.ActivatedAt, &m.ActivatedBy); err != nil {
		return Mechanism{}, err
	}
	return m, json.Unmarshal(cfg, &m.Config)
}

const mechanismColumns = `id, context_id, owner_party_id, state, config,
	created_by_party_id, created_at, activated_at, activated_by`

// getMechanism loads one mechanism by id; forUpdate locks it for the length
// of the activation transaction, so two concurrent activations cannot both
// re-release the same held instructions.
func getMechanism(ctx context.Context, q store.Querier, id string, forUpdate bool) (Mechanism, error) {
	query := `SELECT ` + mechanismColumns + ` FROM mechanisms WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanMechanism(q.QueryRow(ctx, query, id))
}

func mechanismByContext(ctx context.Context, q store.Querier, contextID string) (Mechanism, error) {
	return scanMechanism(q.QueryRow(ctx,
		`SELECT `+mechanismColumns+` FROM mechanisms WHERE context_id = $1`, contextID))
}

func markMechanismActive(ctx context.Context, tx store.Querier, m Mechanism) error {
	_, err := tx.Exec(ctx, `
		UPDATE mechanisms SET state = $2, activated_at = $3, activated_by = $4
		WHERE id = $1`, m.ID, m.State, m.ActivatedAt, m.ActivatedBy)
	return err
}

// ─── mechanism records (the recorded acts) ──────────────────────────────────

type mechanismRecord struct {
	ID           string         `json:"id"`
	MechanismID  string         `json:"mechanismId"`
	Kind         string         `json:"kind"`
	ActorPartyID string         `json:"actorPartyId"`
	Payload      map[string]any `json:"payload,omitempty"`
	At           time.Time      `json:"at"`
}

func insertMechanismRecord(ctx context.Context, tx store.Querier, rec mechanismRecord) error {
	payload, err := json.Marshal(rec.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO mechanism_records (id, mechanism_id, kind, actor_party_id, payload, at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		rec.ID, rec.MechanismID, rec.Kind, rec.ActorPartyID, payload, rec.At)
	return err
}

func mechanismRecords(ctx context.Context, q store.Querier, mechanismID string) ([]mechanismRecord, error) {
	rows, err := q.Query(ctx, `
		SELECT id, mechanism_id, kind, actor_party_id, payload, at
		FROM mechanism_records WHERE mechanism_id = $1 ORDER BY at, id`, mechanismID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (mechanismRecord, error) {
		var rec mechanismRecord
		var payload []byte
		if err := r.Scan(&rec.ID, &rec.MechanismID, &rec.Kind, &rec.ActorPartyID,
			&payload, &rec.At); err != nil {
			return mechanismRecord{}, err
		}
		return rec, json.Unmarshal(payload, &rec.Payload)
	})
}

// factsFor reduces the record trail and the test results to the facts the
// activation conditions read. First act of each kind wins for the timestamp;
// the acts themselves all remain in the trail.
func factsFor(records []mechanismRecord, tests []testDisbursement) mechanismFacts {
	var f mechanismFacts
	first := func(dst **time.Time, at time.Time) {
		if *dst == nil {
			when := at
			*dst = &when
		}
	}
	for _, rec := range records {
		switch rec.Kind {
		case recordReconciliationAgreement:
			first(&f.ReconciliationAgreedAt, rec.At)
		case recordBatchingChoice:
			first(&f.BatchingChosenAt, rec.At)
		case recordQualificationSubmitted:
			first(&f.QualificationSubmitted, rec.At)
		case recordQualificationVerified:
			first(&f.QualificationVerified, rec.At)
		}
	}
	for _, t := range tests {
		if t.State == "SUCCEEDED" {
			first(&f.TestSucceededAt, t.At)
		}
	}
	return f
}

// ─── test disbursements ─────────────────────────────────────────────────────

type testDisbursement struct {
	ID          string      `json:"id"`
	MechanismID string      `json:"mechanismId"`
	RequestedBy string      `json:"requestedBy"`
	AmountMinor int64       `json:"amountMinor"`
	Currency    string      `json:"currency"`
	Destination string      `json:"destination"`
	State       string      `json:"state"`
	RailRef     *string     `json:"railRef,omitempty"`
	Failure     *HeldReason `json:"failure,omitempty"`
	At          time.Time   `json:"at"`
}

func insertTestDisbursement(ctx context.Context, tx store.Querier, t testDisbursement) error {
	var code, reason, owner *string
	if t.Failure != nil {
		code, reason, owner = &t.Failure.Code, &t.Failure.Explanation, &t.Failure.OwnerPartyID
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO test_disbursements (id, mechanism_id, requested_by, amount_minor,
		    currency, destination, state, rail_ref, failure_code, failure_reason,
		    failure_owner, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		t.ID, t.MechanismID, t.RequestedBy, t.AmountMinor, t.Currency, t.Destination,
		t.State, t.RailRef, code, reason, owner, t.At)
	return err
}

func testDisbursements(ctx context.Context, q store.Querier, mechanismID string) ([]testDisbursement, error) {
	rows, err := q.Query(ctx, `
		SELECT id, mechanism_id, requested_by, amount_minor, currency, destination,
		       state, rail_ref, failure_code, failure_reason, failure_owner, at
		FROM test_disbursements WHERE mechanism_id = $1 ORDER BY at, id`, mechanismID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (testDisbursement, error) {
		var t testDisbursement
		var code, reason, owner *string
		if err := r.Scan(&t.ID, &t.MechanismID, &t.RequestedBy, &t.AmountMinor,
			&t.Currency, &t.Destination, &t.State, &t.RailRef,
			&code, &reason, &owner, &t.At); err != nil {
			return testDisbursement{}, err
		}
		if code != nil && reason != nil && owner != nil {
			t.Failure = &HeldReason{Code: *code, Explanation: *reason, OwnerPartyID: *owner}
		}
		return t, nil
	})
}

// ─── held-for-mechanism instructions ────────────────────────────────────────

// heldForMechanism finds the payments a not-yet-live mechanism was holding:
// the obligations every window exit created regardless (f2_9), waiting for
// activation to let disbursement flow.
func heldForMechanism(ctx context.Context, q store.Querier, contextID string) ([]Instruction, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM instructions
		WHERE context_id = $1 AND state = 'HELD' AND held_code = 'mechanism_not_live'
		ORDER BY created_at, id
		FOR UPDATE`, contextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (Instruction, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return Instruction{}, err
		}
		var in Instruction
		return in, json.Unmarshal(doc, &in)
	})
}

// releaseMechanismHeld clears only the mechanism gate. Pricing fields are
// intentionally untouched: activation is not a new pricing event.
func releaseMechanismHeld(in Instruction) Instruction {
	in.State = "RELEASED"
	in.Held = nil
	return in
}

// releaseHeldInstruction moves one held instruction to RELEASED with its
// computed amount, or onto a fresh hold reason — never back to an
// explanationless state. Updates doc and columns together so the two views
// cannot disagree.
func releaseHeldInstruction(ctx context.Context, tx store.Querier, in Instruction) error {
	doc, err := json.Marshal(in)
	if err != nil {
		return err
	}
	var code, reason, owner *string
	if in.Held != nil {
		code, reason, owner = &in.Held.Code, &in.Held.Explanation, &in.Held.OwnerPartyID
	}
	_, err = tx.Exec(ctx, `
		UPDATE instructions SET state = $2, amount_minor = $3, currency = $4,
		       held_code = $5, held_reason = $6, held_owner = $7, doc = $8
		WHERE id = $1`,
		in.ID, in.State, in.AmountMinor, in.Currency, code, reason, owner, doc)
	return err
}

// releaseHeldInstructionAndEnqueue persists the already-decided instruction
// state and schedules the rail send in the same transaction. Callers must set
// the state and hold first; this helper deliberately does no pricing, because
// a retry or mechanism activation must never change a priced obligation.
func releaseHeldInstructionAndEnqueue(ctx context.Context, tx store.Querier, in Instruction) error {
	if err := releaseHeldInstruction(ctx, tx, in); err != nil {
		return err
	}
	if in.State == "RELEASED" {
		return store.Enqueue(ctx, tx, topicRailSend, in)
	}
	return nil
}

// ─── the reconciliation file and statements ─────────────────────────────────

// reconciliationLine is one line of the export contract (f2_5): one line per
// payment instruction, each carrying enough to tie it back — instruction id,
// claim, rail reference — so any mismatch against the rail's own records is
// findable from either side.
type reconciliationLine struct {
	InstructionID string
	ClaimID       string
	UnitID        string
	PartyID       string
	State         string
	AmountMinor   int64
	Currency      string
	ReleasedBy    string
	ReleasedAt    time.Time
	RailState     string
	RailRef       string
	HeldCode      string
	HeldOwner     string
}

func reconciliationLines(ctx context.Context, q store.Querier) ([]reconciliationLine, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, i.claim_id, i.unit_id, i.party_id, i.state, i.amount_minor,
		       i.currency, i.released_by, i.released_at,
		       COALESCE(c.state, ''), COALESCE(c.rail_ref, ''),
		       COALESCE(i.held_code, ''), COALESCE(i.held_owner, '')
		FROM instructions i
		LEFT JOIN compensations c ON c.instruction_id = i.id
		ORDER BY i.created_at, i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (reconciliationLine, error) {
		var l reconciliationLine
		err := r.Scan(&l.InstructionID, &l.ClaimID, &l.UnitID, &l.PartyID, &l.State,
			&l.AmountMinor, &l.Currency, &l.ReleasedBy, &l.ReleasedAt,
			&l.RailState, &l.RailRef, &l.HeldCode, &l.HeldOwner)
		return l, err
	})
}

// statementInstructions is one party's instructions inside one time span,
// across any merge — the same expansion the payments list uses (#100).
func statementInstructions(ctx context.Context, q store.Querier, partyIDs []string,
	from, to time.Time) ([]Instruction, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM instructions
		WHERE party_id = ANY($1) AND released_at >= $2 AND released_at < $3
		ORDER BY released_at, id`, partyIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (Instruction, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return Instruction{}, err
		}
		var in Instruction
		return in, json.Unmarshal(doc, &in)
	})
}
