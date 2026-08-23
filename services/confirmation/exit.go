package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// The four exits from a T=7 window, in one function, on purpose.
//
// W4 says every exit releases payment: confirm, dispute, auto-confirm,
// supervisor-assisted. The cheapest way for that to break is for the four to be
// four code paths, three of which release and one of which — the one nobody
// demos — does not. So there is one path, the route is a parameter, and the
// release is unconditional.
//
// What the route *does* change is the record:
//
//	self, auto, assisted → the claim is ACCEPTED and a credential is issued
//	dispute              → the claim is DISPUTED and no credential is issued
//
// Both branches release payment. A credential asserting a claim the worker
// disputes would be a false statement CREST signed; withholding the money
// because the worker objected would be a punishment for objecting.

type exiter struct {
	db            *store.DB
	evidence      *client.Client
	issuer        *credential.Issuer
	statusListURL string
	clock         interface{ Now() time.Time }
}

type exitResult struct {
	Window     Window            `json:"window"`
	Credential *issuedCredential `json:"credential,omitempty"`
}

type releaseRequest struct {
	ClaimID    string    `json:"claimId"`
	UnitID     string    `json:"unitId"`
	PartyID    string    `json:"partyId"`
	ReleasedBy string    `json:"releasedBy"`
	ReleasedAt time.Time `json:"releasedAt"`
}

func (e *exiter) exit(ctx context.Context, claimID, route string) (exitResult, error) {
	now := e.clock.Now()

	w, err := getWindow(ctx, e.db.Q(), claimID, false)
	if err != nil {
		return exitResult{}, err
	}
	if !w.Open() {
		// Already exited. Idempotent rather than an error: the sweep and a
		// worker's confirmation can race, and the worker should win without
		// anyone seeing a failure.
		var cred *issuedCredential
		if w.CredentialID != nil {
			c, err := getCredential(ctx, e.db.Q(), *w.CredentialID)
			if err == nil {
				cred = &c
			}
		}
		return exitResult{Window: w, Credential: cred}, nil
	}

	// The claim's state is owned by evidence, so it moves there first. If this
	// fails, nothing here has changed and the caller can retry.
	target := schema.ClaimStateACCEPTED
	if route == routeDispute {
		target = schema.ClaimStateDISPUTED
	}
	if err := e.transitionClaim(ctx, claimID, target, route); err != nil {
		return exitResult{}, err
	}

	var issued *issuedCredential
	if route != routeDispute {
		issued, err = e.buildCredential(ctx, w, route, now)
		if err != nil {
			return exitResult{}, err
		}
	}

	err = e.db.InTx(ctx, func(tx store.Querier) error {
		var credID *string
		if issued != nil {
			idx, err := nextStatusIndex(ctx, tx)
			if err != nil {
				return err
			}
			// The index is allocated inside the transaction and then written
			// into the credential, so no two credentials can share a slot —
			// revoking one would otherwise revoke the other.
			if err := issued.setStatusIndex(idx, e.statusListURL, e.issuer, now); err != nil {
				return err
			}
			if err := insertCredential(ctx, tx, *issued); err != nil {
				return err
			}
			credID = &issued.ID
		}
		if err := recordExit(ctx, tx, claimID, route, now, credID); err != nil {
			return err
		}
		// The release is enqueued in the same transaction as the exit. A crash
		// between them is the failure W4 cannot survive, and this is what makes
		// it impossible rather than unlikely.
		return store.Enqueue(ctx, tx, topicPaymentRelease, releaseRequest{
			ClaimID: w.ClaimID, UnitID: w.UnitID, PartyID: w.PartyID,
			ReleasedBy: route, ReleasedAt: now,
		})
	})
	if err != nil {
		return exitResult{}, err
	}

	w, err = getWindow(ctx, e.db.Q(), claimID, false)
	if err != nil {
		return exitResult{}, err
	}
	return exitResult{Window: w, Credential: issued}, nil
}

func (e *exiter) transitionClaim(ctx context.Context, claimID string, to schema.ClaimState, route string) error {
	body := map[string]any{"to": to}
	if route != routeDispute {
		body["route"] = route
	}
	if err := e.evidence.Post(ctx, "/v1/claims/"+claimID+"/transition", body, nil); err != nil {
		return fmt.Errorf("evidence would not move claim %s to %s: %w", claimID, to, err)
	}
	return nil
}

// buildCredential assembles and signs the credential. It reads the unit from
// evidence rather than caching it, so what is signed is what the record
// currently says rather than what it said when the window opened.
func (e *exiter) buildCredential(ctx context.Context, w Window, route string, now time.Time) (*issuedCredential, error) {
	var unit schema.Unit
	if err := e.evidence.Get(ctx, "/v1/units/"+w.UnitID, &unit); err != nil {
		return nil, fmt.Errorf("could not read unit %s: %w", w.UnitID, err)
	}

	// The subject is the Party's own pairwise, deployment-local DID (§4). Not a
	// name, not a national identifier, not the provider's subject — nothing
	// that correlates outside this deployment (W8, W9).
	subjectRef := w.PartyID

	return &issuedCredential{
		ID:         id.New(e.clock, "credential"),
		ClaimID:    w.ClaimID,
		SubjectRef: subjectRef,
		IssuedAt:   now,
		unit:       unit,
		route:      route,
	}, nil
}

// setStatusIndex finishes the credential once its status slot is known, then
// signs it. Signing last is what makes the status entry part of what is signed.
func (c *issuedCredential) setStatusIndex(idx int, listURL string, iss *credential.Issuer, now time.Time) error {
	doc, err := credential.Document(c.ID, iss.ID(), c.SubjectRef, c.unit,
		schema.ClaimConfirmationRoute(c.route), now, c.unit.Definition.ID, listURL, idx, now)
	if err != nil {
		return fmt.Errorf("the credential does not satisfy its own schema: %w", err)
	}
	signed, err := iss.Issue(doc, now)
	if err != nil {
		return err
	}
	digest, err := credential.Digest(signed)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		return err
	}
	c.StatusIndex = idx
	c.Digest = digest
	c.Doc = raw
	return nil
}
