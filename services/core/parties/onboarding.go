package parties

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/store"
)

// Organisation onboarding and assisted enrolment (#20, Blueprint §3).

// Registration states. APPLIED → TERMS_ACCEPTED → APPROVED, or → REJECTED from
// either. There is no path back to APPLIED: an organisation that was rejected
// and reapplies is a new decision on the same record, not the erasure of the
// old one.
const (
	stateApplied       = "APPLIED"
	stateTermsAccepted = "TERMS_ACCEPTED"
	stateApproved      = "APPROVED"
	stateRejected      = "REJECTED"
)

var (
	// ErrAlreadyApplied is a second application from an organisation that
	// already has one. Refused rather than reset: an overwrite would let a
	// rejected organisation clear its own decision by applying again.
	ErrAlreadyApplied = errors.New("this organisation already has an application")

	ErrSelfApproved     = errors.New("an organisation may not approve its own application")
	ErrTermsNotAccepted = errors.New("an organisation must accept terms before it can be approved")
	ErrAlreadyDecided   = errors.New("this application has already been decided")
)

// approvalModel is configuration, not infrastructure.
//
// The layering test: two deployments could reasonably disagree about whether a
// registering organisation is approved by a person or automatically on terms
// acceptance, and both would still be CREST. A pilot with three known partner
// NGOs and a national rollout with four hundred want different answers. So the
// model is read from the environment and the state machine supports both,
// rather than one of them being compiled in.
//
// Manual is the default. An approval that happens by itself is one nobody
// remembers granting, and the safe default for a system whose records decide
// whether someone gets paid is the one that leaves a person's name on it.
type approvalModel string

const (
	approvalManual  approvalModel = "manual"
	approvalOnTerms approvalModel = "on-terms-acceptance"
)

func loadApprovalModel() (approvalModel, error) {
	switch m := approvalModel(config.Str("REGISTRY_ORG_APPROVAL", string(approvalManual))); m {
	case approvalManual, approvalOnTerms:
		return m, nil
	default:
		return "", fmt.Errorf("REGISTRY_ORG_APPROVAL=%q is not a model; want %q or %q",
			m, approvalManual, approvalOnTerms)
	}
}

// Registration is an organisation's application and what was decided about it.
type Registration struct {
	PartyID      string     `json:"partyId"`
	State        string     `json:"state"`
	TermsID      *string    `json:"termsId,omitempty"`
	TermsVersion *int       `json:"termsVersion,omitempty"`
	AcceptedBy   *string    `json:"acceptedBy,omitempty"`
	AcceptedAt   *time.Time `json:"acceptedAt,omitempty"`
	DecidedBy    *string    `json:"decidedBy,omitempty"`
	DecidedAt    *time.Time `json:"decidedAt,omitempty"`
	Reason       *string    `json:"reason,omitempty"`
	AppliedAt    time.Time  `json:"appliedAt"`
}

func insertRegistration(ctx context.Context, tx store.Querier, partyID string, at time.Time) error {
	// Idempotent on the party: a repeated application is the same application.
	// The alternative — a second row, or an overwrite that clears an earlier
	// decision — lets a rejected organisation reset its own state by asking
	// twice.
	affected, err := tx.Exec(ctx, `
		INSERT INTO org_registrations (party_id, state, applied_at)
		VALUES ($1, $2, $3) ON CONFLICT (party_id) DO NOTHING`,
		partyID, stateApplied, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAlreadyApplied
	}
	return nil
}

func getRegistration(ctx context.Context, q store.Querier, partyID string) (Registration, error) {
	var r Registration
	err := q.QueryRow(ctx, `
		SELECT party_id, state, terms_id, terms_version, accepted_by, accepted_at,
		       decided_by, decided_at, reason, applied_at
		FROM org_registrations WHERE party_id = $1`, partyID).
		Scan(&r.PartyID, &r.State, &r.TermsID, &r.TermsVersion, &r.AcceptedBy, &r.AcceptedAt,
			&r.DecidedBy, &r.DecidedAt, &r.Reason, &r.AppliedAt)
	return r, err
}

// acceptTerms records that an organisation agreed to a specific terms version.
//
// The version is not optional and not defaulted to "the latest". Which terms an
// organisation held at a given moment is the fact a verifier walks back to from
// a credential, and "whatever was current" is not a fact anyone can check later.
func acceptTerms(ctx context.Context, tx store.Querier, partyID, termsID string, version int,
	acceptedBy string, at time.Time) (Registration, error) {
	var state string
	if err := tx.QueryRow(ctx,
		`SELECT state FROM org_registrations WHERE party_id = $1 FOR UPDATE`, partyID).
		Scan(&state); err != nil {
		return Registration{}, err
	}
	if state == stateApproved || state == stateRejected {
		return Registration{}, ErrAlreadyDecided
	}
	if _, err := tx.Exec(ctx, `
		UPDATE org_registrations
		SET state = $2, terms_id = $3, terms_version = $4, accepted_by = $5, accepted_at = $6
		WHERE party_id = $1`,
		partyID, stateTermsAccepted, termsID, version, acceptedBy, at); err != nil {
		return Registration{}, err
	}
	return getRegistration(ctx, tx, partyID)
}

// decide approves or rejects, refusing a self-granted approval.
//
// The database carries the same rule as a CHECK constraint. Two enforcements of
// one rule is not redundancy here: the constraint is what holds when a future
// code path forgets, and the code is what produces an error a caller can act on
// instead of a driver message.
func decide(ctx context.Context, tx store.Querier, partyID string, approve bool,
	decidedBy, reason string, at time.Time, model approvalModel) (Registration, error) {
	var state string
	if err := tx.QueryRow(ctx,
		`SELECT state FROM org_registrations WHERE party_id = $1 FOR UPDATE`, partyID).
		Scan(&state); err != nil {
		return Registration{}, err
	}
	if state == stateApproved || state == stateRejected {
		return Registration{}, ErrAlreadyDecided
	}
	if decidedBy == partyID {
		return Registration{}, ErrSelfApproved
	}
	// Approval without accepted terms is an organisation operating under
	// nothing. Rejection needs no terms — refusing an application before it
	// gets that far is a normal outcome.
	if approve && state != stateTermsAccepted {
		return Registration{}, ErrTermsNotAccepted
	}
	_ = model // the model decides who may call this, not what it does; see routes

	next := stateRejected
	if approve {
		next = stateApproved
	}
	if _, err := tx.Exec(ctx, `
		UPDATE org_registrations SET state = $2, decided_by = $3, decided_at = $4, reason = $5
		WHERE party_id = $1`, partyID, next, decidedBy, at, reason); err != nil {
		return Registration{}, err
	}
	return getRegistration(ctx, tx, partyID)
}

// Enrolment records that one party enrolled another.
type Enrolment struct {
	PartyID    string    `json:"partyId"`
	EnrolledBy string    `json:"enrolledBy"`
	ContextID  *string   `json:"contextId,omitempty"`
	Method     string    `json:"method"`
	Note       *string   `json:"note,omitempty"`
	EnrolledAt time.Time `json:"enrolledAt"`
}

func insertEnrolment(ctx context.Context, tx store.Querier, e Enrolment) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO assisted_enrolments (party_id, enrolled_by, context_id, method, note, enrolled_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (party_id) DO NOTHING`,
		e.PartyID, e.EnrolledBy, e.ContextID, e.Method, e.Note, e.EnrolledAt)
	return err
}

func getEnrolment(ctx context.Context, q store.Querier, partyID string) (Enrolment, error) {
	var e Enrolment
	err := q.QueryRow(ctx, `
		SELECT party_id, enrolled_by, context_id, method, note, enrolled_at
		FROM assisted_enrolments WHERE party_id = $1`, partyID).
		Scan(&e.PartyID, &e.EnrolledBy, &e.ContextID, &e.Method, &e.Note, &e.EnrolledAt)
	return e, err
}

// atoiPositive parses a version, refusing zero and negatives rather than
// coercing them. A query parameter that means "version 0" means the caller made
// a mistake, and answering it with version 1 is answering a different question.
func atoiPositive(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("version %d is not positive", n)
	}
	return n, nil
}
