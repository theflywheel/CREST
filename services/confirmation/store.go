package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/credential"
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

func markNotified(ctx context.Context, tx store.Querier, claimID string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE windows SET notified_at = $2 WHERE claim_id = $1`, claimID, at)
	return err
}

// recordReach stores whether the worker was actually told.
func recordReach(ctx context.Context, tx store.Querier, claimID, reach, detail string) error {
	_, err := tx.Exec(ctx,
		`UPDATE windows SET reach = $2, reach_detail = $3 WHERE claim_id = $1`,
		claimID, reach, detail)
	return err
}

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

func insertCredential(ctx context.Context, tx store.Querier, c issuedCredential) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO credentials (id, claim_id, subject_ref, status_index, digest, doc, issued_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		c.ID, c.ClaimID, c.SubjectRef, c.StatusIndex, c.Digest, c.Doc, c.IssuedAt)
	return err
}

type issuedCredential struct {
	ID          string          `json:"id"`
	ClaimID     string          `json:"claimId"`
	SubjectRef  string          `json:"subjectRef"`
	StatusIndex int             `json:"statusIndex"`
	Digest      string          `json:"digest"`
	Doc         json.RawMessage `json:"credential"`
	IssuedAt    time.Time       `json:"issuedAt"`
	RevokedAt   *time.Time      `json:"revokedAt,omitempty"`

	// Carried between building and signing, never stored: the credential is
	// assembled before its status slot is known, and signed after.
	unit  schema.Unit `json:"-"`
	route string      `json:"-"`

	// Resolved once, at build time, from the definitions service. Nil where
	// there is nothing a verifier could check.
	defProof  *schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof `json:"-"`
	skillCode *string                                                              `json:"-"`
	authority *schema.WorkEventCredentialCredentialSubjectIssuerAuthority          `json:"-"`
}

func getCredential(ctx context.Context, q store.Querier, credID string) (issuedCredential, error) {
	var c issuedCredential
	err := q.QueryRow(ctx, `
		SELECT id, claim_id, subject_ref, status_index, digest, doc, issued_at, revoked_at
		FROM credentials WHERE id = $1`, credID).
		Scan(&c.ID, &c.ClaimID, &c.SubjectRef, &c.StatusIndex, &c.Digest, &c.Doc, &c.IssuedAt, &c.RevokedAt)
	return c, err
}

// nextStatusIndex hands out the next slot in the bitstring, inside the caller's
// transaction so two issuances cannot take the same one. Two credentials
// sharing a status index means revoking one revokes both.
func nextStatusIndex(ctx context.Context, tx store.Querier) (int, error) {
	var idx int
	err := tx.QueryRow(ctx,
		`UPDATE status_list SET next_index = next_index + 1 WHERE id = 1 RETURNING next_index - 1`).
		Scan(&idx)
	return idx, err
}

func loadStatusList(ctx context.Context, q store.Querier) (*credential.StatusList, error) {
	var bits []byte
	if err := q.QueryRow(ctx, `SELECT bits FROM status_list WHERE id = 1`).Scan(&bits); err != nil {
		return nil, err
	}
	return credential.FromBytes(bits), nil
}

func saveStatusList(ctx context.Context, tx store.Querier, list *credential.StatusList) error {
	_, err := tx.Exec(ctx, `UPDATE status_list SET bits = $1 WHERE id = 1`, list.Bytes())
	return err
}

func revokeCredential(ctx context.Context, tx store.Querier, credID string, at time.Time) (int, error) {
	var idx int
	err := tx.QueryRow(ctx,
		`UPDATE credentials SET revoked_at = $2 WHERE id = $1 RETURNING status_index`, credID, at).
		Scan(&idx)
	return idx, err
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
