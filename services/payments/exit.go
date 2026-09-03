package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/theflywheel/crest/pkg/client"
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
	db           *store.DB
	evidence     *client.Client
	verification *client.Client
	log          *slog.Logger
	clock        interface{ Now() time.Time }
}

type exitResult struct {
	Window     Window            `json:"window"`
	Credential *issuedCredential `json:"credential,omitempty"`
}

// issueRequest is what this application says to the substrate at a confirming
// exit: which claim's record is confirmed, whose, by which route, when.
// Everything about what a credential *is* — shape, keys, status — lives on the
// other side of the boundary (#137).
type issueRequest struct {
	ClaimID   string    `json:"claimId"`
	UnitID    string    `json:"unitId"`
	PartyID   string    `json:"partyId"`
	ContextID string    `json:"contextId"`
	Route     string    `json:"route"`
	At        time.Time `json:"at"`
}

// issuedCredential mirrors verification's answer, so an exit can hand the
// caller the credential it just asked for without owning its shape.
type issuedCredential struct {
	ID          string          `json:"id"`
	ClaimID     string          `json:"claimId"`
	SubjectRef  string          `json:"subjectRef"`
	StatusIndex int             `json:"statusIndex"`
	Digest      string          `json:"digest"`
	Doc         json.RawMessage `json:"credential"`
	IssuedAt    time.Time       `json:"issuedAt"`
	RevokedAt   *time.Time      `json:"revokedAt,omitempty"`
}

type releaseRequest struct {
	ClaimID string `json:"claimId"`
	UnitID  string `json:"unitId"`
	PartyID string `json:"partyId"`
	// The window's context rides along so the instruction knows which
	// project's mechanism governs its DISBURSEMENT (f2_9). It has no say in
	// whether the release happens — all four exits release, always (W4).
	ContextID  string    `json:"contextId"`
	ReleasedBy string    `json:"releasedBy"`
	ReleasedAt time.Time `json:"releasedAt"`
}

func (e *exiter) exit(ctx context.Context, claimID, route string) (exitResult, error) {
	now := e.clock.Now()

	// The whole exit happens under a FOR UPDATE lock on the window row. The
	// scheduled sweep and a worker's confirmation can arrive at T=7 within the
	// same second; without the lock both see an open window, both issue a
	// credential, and one exit record silently overwrites the other — in the
	// worst interleaving an auto-confirm overwrites a dispute. With it, the
	// loser blocks until the winner commits, then sees a closed window and
	// takes the idempotent branch below. The lock is held across the calls to
	// evidence and definitions; that is the price of one claim never exiting
	// twice, and only exits contend for it.
	var issued *issuedCredential
	alreadyExited := false
	err := e.db.InTx(ctx, func(tx store.Querier) error {
		w, err := getWindow(ctx, tx, claimID, true)
		if err != nil {
			return err
		}
		if !w.Open() {
			alreadyExited = true
			return nil
		}

		// The claim's state is owned by evidence, so it moves there first. If
		// this fails, the transaction rolls back and nothing here has changed;
		// the caller can retry.
		target := schema.ClaimStateACCEPTED
		if route == routeDispute {
			target = schema.ClaimStateDISPUTED
		}
		if err := e.transitionClaim(ctx, claimID, target, route); err != nil {
			return err
		}

		if route == routeDispute {
			// A credential can exist for a still-open window in exactly one
			// case: a confirming exit crashed after verification committed
			// the credential and before this window recorded the exit. The
			// claim is DISPUTED now, and a standing credential would be a
			// false statement CREST signed — so the orphan is revoked before
			// the dispute exit commits. This is distinct from a dispute after
			// auto-confirm (the alreadyExited branch below), where the
			// credential was legitimately issued and deliberately stands.
			var orphan issuedCredential
			switch err := e.verification.Get(ctx,
				"/internal/credentials/by-claim/"+url.PathEscape(claimID), &orphan); {
			case err == nil && orphan.RevokedAt == nil:
				if err := e.verification.Post(ctx,
					"/internal/credentials/"+url.PathEscape(orphan.ID)+"/revoke", nil, nil); err != nil {
					return fmt.Errorf("could not revoke the crash-orphaned credential for claim %s: %w", claimID, err)
				}
				e.log.Warn("revoked a crash-orphaned credential at dispute",
					"claim", claimID, "credential", orphan.ID)
			case err == nil:
				// Already revoked; nothing standing.
			case client.Code(err) == http.StatusNotFound:
				// The normal case: no credential was ever issued.
			default:
				// Refused rather than skipped: committing this dispute without
				// knowing would leave a possibly-standing credential for a
				// disputed claim, silently.
				return fmt.Errorf("could not check for an orphaned credential for claim %s: %w", claimID, err)
			}
		}

		var credID *string
		if route != routeDispute {
			// Issuance is the substrate's act, not this application's (#137):
			// verification holds the keys, the status list and the credential
			// record, and this service asks for exactly one credential per
			// confirmed claim. The call is idempotent by claim, so a crash
			// between it succeeding and this transaction committing is
			// recovered by the retry reading the same credential back.
			var c issuedCredential
			if err := e.verification.Post(ctx, "/internal/credentials/issue", issueRequest{
				ClaimID: w.ClaimID, UnitID: w.UnitID, PartyID: w.PartyID,
				ContextID: w.ContextID, Route: route, At: now,
			}, &c); err != nil {
				return fmt.Errorf("verification would not issue for claim %s: %w", w.ClaimID, err)
			}
			issued = &c
			credID = &c.ID
		}
		if err := recordExit(ctx, tx, claimID, route, now, credID); err != nil {
			return err
		}
		// The release is enqueued in the same transaction as the exit. A crash
		// between them is the failure W4 cannot survive, and this is what makes
		// it impossible rather than unlikely.
		return store.Enqueue(ctx, tx, topicPaymentRelease, releaseRequest{
			ClaimID: w.ClaimID, UnitID: w.UnitID, PartyID: w.PartyID,
			ContextID: w.ContextID, ReleasedBy: route, ReleasedAt: now,
		})
	})
	if err != nil {
		return exitResult{}, err
	}

	if alreadyExited {
		// Already exited. Idempotent rather than an error: the sweep and a
		// worker's confirmation can race, and the worker should win without
		// anyone seeing a failure.
		//
		// A dispute is the exception, and W3 is why. Silence is not consent
		// against the worker: the seven days are a window for objecting, not a
		// deadline for noticing, so a claim that auto-confirmed must still be
		// disputable afterwards. The payment is already out and stays out —
		// what changes is what the record says.
		if route == routeDispute {
			if err := e.transitionClaim(ctx, claimID, schema.ClaimStateDISPUTED, route); err != nil {
				return exitResult{}, err
			}
		}
	}

	w, err := getWindow(ctx, e.db.Q(), claimID, false)
	if err != nil {
		return exitResult{}, err
	}
	var cred *issuedCredential
	if issued != nil {
		cred = issued
	} else if w.CredentialID != nil {
		var c issuedCredential
		if err := e.verification.Get(ctx, "/v1/credentials/"+url.PathEscape(*w.CredentialID), &c); err == nil {
			cred = &c
		}
	}
	return exitResult{Window: w, Credential: cred}, nil
}

func (e *exiter) transitionClaim(ctx context.Context, claimID string, to schema.ClaimState, route string) error {
	body := map[string]any{"to": to}
	if route != routeDispute {
		body["route"] = route
	}
	if err := e.evidence.Post(ctx, "/internal/claims/"+claimID+"/transition", body, nil); err != nil {
		return fmt.Errorf("evidence would not move claim %s to %s: %w", claimID, to, err)
	}
	return nil
}
