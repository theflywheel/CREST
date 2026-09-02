package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

const (
	topicNotifyClaim    = "notify.claim"
	topicPaymentRelease = "payment.release"
)

// Window is a claim's confirmation window.
type Window struct {
	ClaimID           string     `json:"claimId"`
	UnitID            string     `json:"unitId"`
	PartyID           string     `json:"partyId"`
	ContextID         string     `json:"contextId"`
	DefinitionID      string     `json:"definitionId"`
	DefinitionVersion int        `json:"definitionVersion"`
	OpenedAt          time.Time  `json:"openedAt"`
	ClosesAt          time.Time  `json:"closesAt"`
	NotifiedAt        *time.Time `json:"notifiedAt,omitempty"`
	ExitRoute         *string    `json:"exitRoute,omitempty"`
	ExitedAt          *time.Time `json:"exitedAt,omitempty"`
	PaymentReleasedAt *time.Time `json:"paymentReleasedAt,omitempty"`
	CredentialID      *string    `json:"credentialId,omitempty"`

	// Reach is whether the worker was actually told, as opposed to whether a
	// message was queued. Nil means not yet established.
	Reach       *string    `json:"reach,omitempty"`
	ReachDetail *string    `json:"reachDetail,omitempty"`
	EscalatedAt *time.Time `json:"escalatedAt,omitempty"`
}

// Open is true while the window is still running.
func (w Window) Open() bool { return w.ExitRoute == nil }

const windowColumns = `claim_id, unit_id, party_id, context_id, definition_id, definition_version,
	opened_at, closes_at, notified_at, exit_route, exited_at, payment_released_at, credential_id,
	reach, reach_detail, escalated_at`

func scanWindow(r store.Row) (Window, error) {
	var w Window
	return w, r.Scan(&w.ClaimID, &w.UnitID, &w.PartyID, &w.ContextID, &w.DefinitionID,
		&w.DefinitionVersion, &w.OpenedAt, &w.ClosesAt, &w.NotifiedAt, &w.ExitRoute,
		&w.ExitedAt, &w.PaymentReleasedAt, &w.CredentialID,
		&w.Reach, &w.ReachDetail, &w.EscalatedAt)
}

// insertWindow is idempotent on the claim. The message that creates it is
// delivered at-least-once, and a second window for one claim would mean a
// second notification and a second payment.
func insertWindow(ctx context.Context, tx store.Querier, w Window) (bool, error) {
	affected, err := tx.Exec(ctx, `
		INSERT INTO windows (claim_id, unit_id, party_id, context_id, definition_id,
		                     definition_version, opened_at, closes_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (claim_id) DO NOTHING`,
		w.ClaimID, w.UnitID, w.PartyID, w.ContextID, w.DefinitionID,
		w.DefinitionVersion, w.OpenedAt, w.ClosesAt)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func getWindow(ctx context.Context, q store.Querier, claimID string, forUpdate bool) (Window, error) {
	sql := `SELECT ` + windowColumns + ` FROM windows WHERE claim_id = $1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	rows, err := q.Query(ctx, sql, claimID)
	if err != nil {
		return Window{}, err
	}
	defer rows.Close()
	w, err := store.CollectOne(rows, scanWindow)
	return w, err
}

// dueWindows are the ones whose time has run out. The sweep acts on these, and
// nothing else decides when a window is due — the clock does.
func dueWindows(ctx context.Context, q store.Querier, now time.Time, limit int) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NULL AND closes_at <= $1
		  -- Never auto-confirm a window whose worker was not reached. Silence
		  -- is only consent-shaped if the person had a chance to break it.
		  AND (reach IS NULL OR reach = 'reached')
		ORDER BY closes_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanWindow)
}

// unreleased finds windows that exited but whose payment never went out.
//
// This should always be empty. It exists because "should always be empty" is
// worth being able to check: W4 is a promise, and a promise with no query
// behind it is a hope.
func unreleased(ctx context.Context, q store.Querier) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NOT NULL AND payment_released_at IS NULL
		ORDER BY exited_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanWindow)
}

// markNotified and recordReach were the notification channel's writers.
// #150 dropped the channel and left the columns (notified_at, reach,
// reach_detail) dormant as the seam for a returning one; the writers went
// with the channel — resurrect them from history when a channel returns.

func markEscalated(ctx context.Context, tx store.Querier, claimID string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE windows SET escalated_at = $2 WHERE claim_id = $1`, claimID, at)
	return err
}

// unreachedWindows are the ones whose time has run out on a worker who was
// never actually told. They are not auto-confirmed; they are surfaced, the way
// a held payment is.
func unreachedWindows(ctx context.Context, q store.Querier, now time.Time) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NULL AND closes_at <= $1 AND reach = 'unreached'
		ORDER BY closes_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanWindow)
}

func recordExit(ctx context.Context, tx store.Querier, claimID, route string,
	at time.Time, credentialID *string) error {
	_, err := tx.Exec(ctx, `
		UPDATE windows
		SET exit_route = $2, exited_at = $3, credential_id = $4, payment_released_at = $3
		WHERE claim_id = $1`, claimID, route, at, credentialID)
	return err
}

func insertContest(ctx context.Context, tx store.Querier, c schema.Contest) error {
	doc, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO contests (id, target_kind, target_id, raised_by, reason, state, doc, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.ID, string(c.Target.Kind), c.Target.ID, c.RaisedByPartyID, c.Reason,
		string(c.State), doc, c.RaisedAt)
	return err
}

// ContestStanding is what a stranger may learn about a dispute.
//
// Deliberately not the Contest itself. A contest carries a free-text reason and
// the party who raised it, and both are ordinarily the worker saying something
// about their own record — "it was six, not nine". That is between the worker
// and the programme. A verifier's legitimate interest is that the record is
// disputed and where the dispute stands, not what the worker said or that it
// was the worker who said it.
//
// So this is a projection, not a filter applied at the edge: there is no path
// that returns the reason to a caller, rather than a path that usually
// remembers not to.
type ContestStanding struct {
	State    string    `json:"state"`
	RaisedAt time.Time `json:"raisedAt"`
}

// contestsAgainst returns the standing of every contest against one target.
//
// Plural because a claim can be disputed more than once — a dispute rejected in
// March does not prevent one in June, and a verifier who saw only the latest
// would miss a pattern that is the whole point of keeping them.
func contestsAgainst(ctx context.Context, q store.Querier, targetKind, targetID string) ([]ContestStanding, error) {
	rows, err := q.Query(ctx, `
		SELECT state, created_at FROM contests
		WHERE target_kind = $1 AND target_id = $2
		ORDER BY created_at`, targetKind, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (ContestStanding, error) {
		var c ContestStanding
		return c, r.Scan(&c.State, &c.RaisedAt)
	})
}

// windowsFor returns every confirmation window belonging to a worker, across
// any merge (#100).
//
// This is the confirmation service's answer to "show me my record": each row
// carries how the window ended, whether the payment was released, and the
// credential it issued. Until now the service could only be asked about one
// claim at a time, so there was no way to ask it about a person at all — which
// made the merge gap invisible here rather than absent.
func windowsFor(ctx context.Context, q store.Querier, partyIDs []string) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		 WHERE party_id = ANY($1)
		 ORDER BY opened_at, claim_id`, partyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanWindow)
}
