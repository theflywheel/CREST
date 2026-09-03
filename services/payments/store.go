package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

const topicRailSend = "rail.send"

// Instruction is what should be paid.
type Instruction struct {
	ID      string `json:"id"`
	ClaimID string `json:"claimId"`
	UnitID  string `json:"unitId"`
	PartyID string `json:"partyId"`
	// ContextID is which project's mechanism governs disbursement (f2_9).
	// Empty on instructions released before it was recorded; those predate
	// the mechanism gate and are not gated by it.
	ContextID   string    `json:"contextId,omitempty"`
	AmountMinor int64     `json:"amountMinor"`
	Currency    string    `json:"currency"`
	ReleasedBy  string    `json:"releasedBy"`
	ReleasedAt  time.Time `json:"releasedAt"`
	State       string    `json:"state"`

	// Held is present exactly when State is HELD. A worker must never see a
	// missing payment with no explanation attached (W10), so this carries the
	// explanation and the person who owns it — not a code someone has to look up.
	Held *HeldReason `json:"held,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

// HeldReason is why a payment did not go out, and who is responsible for it.
type HeldReason struct {
	Code         string `json:"code"`
	Explanation  string `json:"explanation"`
	OwnerPartyID string `json:"ownerPartyId"`
}

// Compensation is what actually happened on the rail.
type Compensation struct {
	ID            string      `json:"id"`
	InstructionID string      `json:"instructionId"`
	UnitID        string      `json:"unitId"`
	AmountMinor   int64       `json:"amountMinor"`
	Currency      string      `json:"currency"`
	RailRef       *string     `json:"railRef,omitempty"`
	State         string      `json:"state"`
	Failure       *HeldReason `json:"failure,omitempty"`
	ConfirmedAt   *time.Time  `json:"confirmedAt,omitempty"`
	CreatedAt     time.Time   `json:"createdAt"`
}

// insertInstruction is idempotent on the claim. Returns false when one already
// existed, which is the normal outcome of an at-least-once redelivery and not
// an error anybody should see.
func insertInstruction(ctx context.Context, tx store.Querier, in Instruction) (bool, error) {
	doc, err := json.Marshal(in)
	if err != nil {
		return false, err
	}
	var code, reason, owner *string
	if in.Held != nil {
		code, reason, owner = &in.Held.Code, &in.Held.Explanation, &in.Held.OwnerPartyID
	}
	affected, err := tx.Exec(ctx, `
		INSERT INTO instructions (id, claim_id, unit_id, party_id, context_id, amount_minor,
		                          currency, released_by, released_at, state, held_code,
		                          held_reason, held_owner, doc, created_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (claim_id) DO NOTHING`,
		in.ID, in.ClaimID, in.UnitID, in.PartyID, in.ContextID, in.AmountMinor, in.Currency,
		in.ReleasedBy, in.ReleasedAt, in.State, code, reason, owner, doc, in.CreatedAt)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func getInstructionByClaim(ctx context.Context, q store.Querier, claimID string) (Instruction, error) {
	var doc []byte
	err := q.QueryRow(ctx, `SELECT doc FROM instructions WHERE claim_id = $1`, claimID).Scan(&doc)
	if err != nil {
		return Instruction{}, err
	}
	var in Instruction
	return in, json.Unmarshal(doc, &in)
}

// listInstructions returns a worker's payment instructions, across any merge
// (#100). See listClaims in the evidence service for why this takes a list:
// instructions raised before a merge still name the absorbed party, and a
// worker whose duplicate was closed must not find their payments split across
// two records neither of which is complete.
func listInstructions(ctx context.Context, q store.Querier, partyIDs []string, state string) ([]Instruction, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM instructions
		-- COALESCE for the same reason as listClaims: a NULL array is not an
		-- empty one, and cardinality(NULL) is NULL.
		WHERE (COALESCE(cardinality($1::text[]), 0) = 0 OR party_id = ANY($1))
		  AND ($2 = '' OR state = $2)
		ORDER BY created_at, id`, partyIDs, state)
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

func upsertCompensation(ctx context.Context, tx store.Querier, c Compensation) error {
	doc, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var code, reason, owner *string
	if c.Failure != nil {
		code, reason, owner = &c.Failure.Code, &c.Failure.Explanation, &c.Failure.OwnerPartyID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO compensations (id, instruction_id, unit_id, amount_minor, currency, rail_ref,
		                           state, failure_code, failure_reason, failure_owner,
		                           confirmed_at, doc, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (instruction_id) DO UPDATE
		SET state = EXCLUDED.state, rail_ref = EXCLUDED.rail_ref,
		    failure_code = EXCLUDED.failure_code, failure_reason = EXCLUDED.failure_reason,
		    failure_owner = EXCLUDED.failure_owner, confirmed_at = EXCLUDED.confirmed_at,
		    doc = EXCLUDED.doc`,
		c.ID, c.InstructionID, c.UnitID, c.AmountMinor, c.Currency, c.RailRef,
		c.State, code, reason, owner, c.ConfirmedAt, doc, c.CreatedAt)
	return err
}

// reconcile compares what was instructed against what was confirmed. Every gap
// gets one reason and one owner (§10) — which is why a gap with neither is
// itself reported as a gap.
type gap struct {
	InstructionID string      `json:"instructionId"`
	ClaimID       string      `json:"claimId"`
	PartyID       string      `json:"partyId"`
	AmountMinor   int64       `json:"amountMinor"`
	Currency      string      `json:"currency"`
	Reason        *HeldReason `json:"reason,omitempty"`
	State         string      `json:"state"`
}

func reconcile(ctx context.Context, q store.Querier) ([]gap, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id, i.claim_id, i.party_id, i.amount_minor, i.currency,
		       COALESCE(c.state, CASE WHEN i.state = 'HELD' THEN 'HELD' ELSE 'NOT_SENT' END),
		       COALESCE(c.failure_code, i.held_code),
		       COALESCE(c.failure_reason, i.held_reason),
		       COALESCE(c.failure_owner, i.held_owner)
		FROM instructions i
		LEFT JOIN compensations c ON c.instruction_id = i.id
		WHERE c.state IS DISTINCT FROM 'CONFIRMED'
		ORDER BY i.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (gap, error) {
		var g gap
		var code, reason, owner *string
		if err := r.Scan(&g.InstructionID, &g.ClaimID, &g.PartyID, &g.AmountMinor,
			&g.Currency, &g.State, &code, &reason, &owner); err != nil {
			return gap{}, err
		}
		if code != nil && reason != nil && owner != nil {
			g.Reason = &HeldReason{Code: *code, Explanation: *reason, OwnerPartyID: *owner}
		}
		return g, nil
	})
}
