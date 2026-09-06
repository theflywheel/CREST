package attestation

import (
	"context"
	"encoding/json"
	"fmt"
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
	Reach                   *string    `json:"reach,omitempty"`
	ReachDetail             *string    `json:"reachDetail,omitempty"`
	EscalatedAt             *time.Time `json:"escalatedAt,omitempty"`
	ReviewStartedAt         *time.Time `json:"reviewStartedAt,omitempty"`
	AcknowledgedAt          *time.Time `json:"acknowledgedAt,omitempty"`
	AcknowledgedBy          *string    `json:"acknowledgedBy,omitempty"`
	AcknowledgementReason   *string    `json:"acknowledgementReason,omitempty"`
	AcknowledgementEvidence *string    `json:"acknowledgementEvidence,omitempty"`
	SupportOwner            *string    `json:"supportOwner,omitempty"`
	SupportAssignedAt       *time.Time `json:"supportAssignedAt,omitempty"`
	SupportReason           *string    `json:"supportReason,omitempty"`
	reviewTokenHash         *string
}

// Open is true while the window is still running.
func (w Window) Open() bool { return w.ExitRoute == nil }

const windowColumns = `claim_id, unit_id, party_id, context_id, definition_id, definition_version,
	opened_at, closes_at, notified_at, exit_route, exited_at, payment_released_at, credential_id,
	reach, reach_detail, escalated_at, review_started_at, review_token_hash, acknowledged_at,
	acknowledged_by, acknowledgement_reason, acknowledgement_evidence, support_owner,
	support_assigned_at, support_reason`

func scanWindow(r store.Row) (Window, error) {
	var w Window
	return w, r.Scan(&w.ClaimID, &w.UnitID, &w.PartyID, &w.ContextID, &w.DefinitionID,
		&w.DefinitionVersion, &w.OpenedAt, &w.ClosesAt, &w.NotifiedAt, &w.ExitRoute,
		&w.ExitedAt, &w.PaymentReleasedAt, &w.CredentialID,
		&w.Reach, &w.ReachDetail, &w.EscalatedAt, &w.ReviewStartedAt, &w.reviewTokenHash,
		&w.AcknowledgedAt, &w.AcknowledgedBy, &w.AcknowledgementReason,
		&w.AcknowledgementEvidence, &w.SupportOwner, &w.SupportAssignedAt, &w.SupportReason)
}

// insertWindow is idempotent on the claim. The message that creates it is
// delivered at-least-once, and a second window for one claim would mean a
// second notification and a second payment.
func insertWindow(ctx context.Context, tx store.Querier, w Window) (bool, error) {
	affected, err := tx.Exec(ctx, `
		INSERT INTO windows (claim_id, unit_id, party_id, context_id, definition_id,
		                     definition_version, opened_at, closes_at, review_token_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (claim_id) DO NOTHING`,
		w.ClaimID, w.UnitID, w.PartyID, w.ContextID, w.DefinitionID,
		w.DefinitionVersion, w.OpenedAt, w.ClosesAt, w.reviewTokenHash)
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
func dueWindows(ctx context.Context, q store.Querier, now time.Time, contextID string, limit int) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NULL AND closes_at <= $1
		  -- A delivery result is required. NULL means the notification system
		  -- has not established reach and is therefore not consent-shaped.
		  AND reach = 'reached'
		  AND ($2 = '' OR context_id = $2)
		ORDER BY closes_at LIMIT $3`, now, contextID, limit)
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
func unreleased(ctx context.Context, q store.Querier, contextID string) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NOT NULL AND payment_released_at IS NULL
		  AND ($1 = '' OR context_id = $1)
		ORDER BY exited_at`, contextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, scanWindow)
}

// recordReach is called by the notification adapter after it has a concrete
// delivery outcome. It records both the outcome and the time the notification
// attempt was made; merely enqueueing a message must never set either field.
func recordReach(ctx context.Context, tx store.Querier, claimID, reach, detail string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE windows SET notified_at = COALESCE(notified_at, $2),
			reach = $3, reach_detail = NULLIF($4, '')
		WHERE claim_id = $1 AND exit_route IS NULL AND acknowledged_at IS NULL
		  AND (reach IS NULL OR reach = 'unreached')`, claimID, at, reach, detail)
	return err
}

func recordNotificationAttempt(ctx context.Context, tx store.Querier, claimID, detail string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE windows SET notified_at = COALESCE(notified_at, $2),
			reach_detail = NULLIF($3, '')
		WHERE claim_id = $1 AND exit_route IS NULL AND acknowledged_at IS NULL`, claimID, at, detail)
	return err
}

func recordAcknowledgement(ctx context.Context, tx store.Querier, claimID string, at time.Time,
	by, reason, evidence string, closesAt time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE windows SET reach = 'reached', reach_detail = $3,
			notified_at = COALESCE(notified_at, $2), review_started_at = $2,
			closes_at = $4, acknowledged_at = $2, acknowledged_by = $5,
			acknowledgement_reason = $3, acknowledgement_evidence = $6,
			review_token_hash = NULL
		WHERE claim_id = $1 AND exit_route IS NULL`,
		claimID, at, reason, closesAt, by, evidence)
	return err
}

func markEscalated(ctx context.Context, tx store.Querier, claimID, owner, reason string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE windows SET escalated_at = COALESCE(escalated_at, $2),
			support_owner = COALESCE(support_owner, $3),
			support_assigned_at = COALESCE(support_assigned_at, $2),
			support_reason = COALESCE(support_reason, $4)
		WHERE claim_id = $1`, claimID, at, owner, reason)
	return err
}

// unreachedWindows are the ones whose time has run out without a positive
// delivery result. Both an explicit failure and NULL (no callback because the
// notification channel is absent or still pending) are surfaced, never
// auto-confirmed.
func unreachedWindows(ctx context.Context, q store.Querier, now time.Time, contextID string) ([]Window, error) {
	rows, err := q.Query(ctx, `
		SELECT `+windowColumns+` FROM windows
		WHERE exit_route IS NULL AND closes_at <= $1 AND ($2 = '' OR context_id = $2)
		  AND (reach IS NULL OR reach = 'unreached')
		ORDER BY closes_at`, now, contextID)
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
		SET exit_route = $2, exited_at = $3, credential_id = $4
		WHERE claim_id = $1`, claimID, route, at, credentialID)
	return err
}

// markPaymentReleased is deliberately separate from recordExit. An exit only
// queues an instruction; payment_released_at means the payment service has
// durably accepted that instruction and is therefore recorded only after the
// subscriber acknowledges it.
func markPaymentReleased(ctx context.Context, db *store.DB, claimID string, at time.Time) error {
	if db == nil {
		return fmt.Errorf("mark payment released: database unavailable")
	}
	return db.InTx(ctx, func(tx store.Querier) error {
		_, err := tx.Exec(ctx, `
			UPDATE windows SET payment_released_at = COALESCE(payment_released_at, $2)
			WHERE claim_id = $1 AND exit_route IS NOT NULL`, claimID, at)
		return err
	})
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

// ContestDecision records an authorized decision on a contest.
type ContestDecision struct {
	ID        string    `json:"id"`
	ContestID string    `json:"contestId"`
	Decision  string    `json:"decision"`
	DecidedBy string    `json:"decidedBy"`
	Reason    string    `json:"reason"`
	Evidence  string    `json:"evidence"`
	DecidedAt time.Time `json:"decidedAt"`
}

// ContestOutcome is the private contest record and its authorized decisions.
type ContestOutcome struct {
	Contest   schema.Contest    `json:"contest"`
	Decisions []ContestDecision `json:"decisions"`
}

// CorrectionEvent records an authorized contest correction emitted to the outbox.
type CorrectionEvent struct {
	ID             string    `json:"id"`
	ContestID      string    `json:"contestId"`
	DecisionID     string    `json:"decisionId"`
	ClaimID        string    `json:"claimId"`
	CredentialID   *string   `json:"credentialId,omitempty"`
	ReplacementRef string    `json:"replacementRef,omitempty"`
	Reason         string    `json:"reason"`
	Evidence       string    `json:"evidence"`
	EmittedAt      time.Time `json:"emittedAt"`
}

func getContest(ctx context.Context, q store.Querier, contestID string) (schema.Contest, error) {
	var doc []byte
	if err := q.QueryRow(ctx, `SELECT doc FROM contests WHERE id = $1`, contestID).Scan(&doc); err != nil {
		return schema.Contest{}, err
	}
	var c schema.Contest
	return c, json.Unmarshal(doc, &c)
}

func contestsForClaim(ctx context.Context, q store.Querier, claimID string) ([]ContestOutcome, error) {
	rows, err := q.Query(ctx, `SELECT doc FROM contests WHERE target_kind = 'claim' AND target_id = $1 ORDER BY created_at, id`, claimID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (ContestOutcome, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return ContestOutcome{}, err
		}
		var c schema.Contest
		if err := json.Unmarshal(doc, &c); err != nil {
			return ContestOutcome{}, err
		}
		return ContestOutcome{Contest: c, Decisions: []ContestDecision{}}, nil
	})
}

func insertContestDecision(ctx context.Context, tx store.Querier, d ContestDecision) error {
	doc, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO contest_decisions (id, contest_id, decision, decided_by, reason, evidence, decided_at, doc)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, d.ID, d.ContestID, d.Decision, d.DecidedBy,
		d.Reason, d.Evidence, d.DecidedAt, doc)
	return err
}

func insertCorrectionEvent(ctx context.Context, tx store.Querier, e CorrectionEvent) error {
	doc, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO correction_events
			(id, contest_id, decision_id, claim_id, credential_id, replacement_ref,
			 reason, evidence, emitted_at, doc)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, e.ID, e.ContestID, e.DecisionID,
		e.ClaimID, e.CredentialID, e.ReplacementRef, e.Reason, e.Evidence, e.EmittedAt, doc)
	return err
}

func contestDecisions(ctx context.Context, q store.Querier, contestID string) ([]ContestDecision, error) {
	rows, err := q.Query(ctx, `SELECT doc FROM contest_decisions WHERE contest_id = $1 ORDER BY decided_at, id`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (ContestDecision, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return ContestDecision{}, err
		}
		var d ContestDecision
		return d, json.Unmarshal(doc, &d)
	})
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
