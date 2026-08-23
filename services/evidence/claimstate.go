package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// The claim state machine.
//
// It lives here, in the service that owns the record, rather than in
// confirmation which owns the *process*. One state machine with one owner: two
// services that both write a state are two services that will eventually
// disagree about it, and on a claim that disagreement is a payment.
//
// Confirmation drives the transitions over HTTP. That is deliberate — it means
// every legal move is expressible as a request, and the harness can make any of
// them without reaching into a database.

// ErrIllegalTransition names a move the machine does not permit.
var ErrIllegalTransition = errors.New("illegal claim transition")

// legal is the whole machine, written as data so it can be read in one glance.
//
// Two entries deserve their reasons stated:
//
//   - ACCEPTED → DISPUTED exists because silence is not consent against the
//     worker (W3). A claim that auto-confirmed at T=7 must still be disputable
//     afterwards, or the seven days become a deadline for noticing rather than
//     a window for objecting.
//   - DISPUTED → ACCEPTED exists because a dispute can be resolved in favour of
//     the record. What does *not* exist is any transition that removes the
//     unit, and no transition here withholds payment: a dispute contests the
//     record, it does not contest the money (W4).
var legal = map[schema.ClaimState][]schema.ClaimState{
	schema.ClaimStateDRAFT:    {schema.ClaimStateNOTIFIED, schema.ClaimStateACCEPTED, schema.ClaimStateDISPUTED},
	schema.ClaimStateNOTIFIED: {schema.ClaimStateACCEPTED, schema.ClaimStateDISPUTED},
	schema.ClaimStateACCEPTED: {schema.ClaimStateDISPUTED},
	schema.ClaimStateDISPUTED: {schema.ClaimStateACCEPTED},
}

func permitted(from, to schema.ClaimState) bool {
	for _, next := range legal[from] {
		if next == to {
			return true
		}
	}
	return false
}

// transitionClaim moves a claim and returns it. The row is locked for the
// duration, so two confirmations arriving together cannot both see NOTIFIED.
func transitionClaim(ctx context.Context, tx store.Querier, claimID string,
	to schema.ClaimState, mutate func(*schema.Claim)) (schema.Claim, error) {
	var doc []byte
	err := tx.QueryRow(ctx, `SELECT doc FROM claims WHERE id = $1 FOR UPDATE`, claimID).Scan(&doc)
	if err != nil {
		return schema.Claim{}, err
	}
	var c schema.Claim
	if err := json.Unmarshal(doc, &c); err != nil {
		return schema.Claim{}, err
	}

	// Idempotent by design: the outbox is at-least-once, so a redelivered
	// "confirm" must be a no-op rather than an error a relay retries forever.
	if c.State == to {
		return c, nil
	}
	if !permitted(c.State, to) {
		return schema.Claim{}, fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, c.State, to)
	}

	c.State = to
	if mutate != nil {
		mutate(&c)
	}
	updated, err := json.Marshal(c)
	if err != nil {
		return schema.Claim{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE claims SET state = $2, doc = $3 WHERE id = $1`, claimID, string(c.State), updated); err != nil {
		return schema.Claim{}, err
	}
	return c, nil
}
