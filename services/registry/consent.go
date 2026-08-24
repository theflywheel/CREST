package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/store"
)

// Enrolment consent (§9, #24).
//
// The first of the three consent moments, and the one this whole issue is
// about: "voice recording is a valid capture for non-literate workers". That is
// not a convenience. For a worker who cannot read the form, a recording of them
// agreeing is the only consent record that is actually theirs — anything else
// is somebody else's account of what they said.
//
// Two properties are enforced rather than documented. A voice consent with no
// recording is refused, because it would be a claim that somebody spoke with
// nothing to show. And withdrawal has an effect: `resolve` reports it and
// evidence refuses to record anything new about that worker, because §9 defines
// enrolment consent as the right to fetch and hold evidence, and a right that
// cannot be taken back was never a right.
//
// What withdrawal does NOT do is touch work already recorded. Consent governs
// what may be collected next. Deleting the record of work somebody did because
// they later withdrew would make withdrawal cost them their history, which is a
// penalty for exercising it.

// ConsentState is what other services need to know, and all they need to know.
type ConsentState string

const (
	// ConsentNone means no enrolment consent has ever been recorded. Distinct
	// from withdrawn: one is a gap, the other is a decision.
	ConsentNone ConsentState = "NONE"
	// ConsentGranted is a live enrolment consent.
	ConsentGranted ConsentState = "GRANTED"
	// ConsentWithdrawn means it was granted and taken back.
	ConsentWithdrawn ConsentState = "WITHDRAWN"
)

// Consent is one recorded consent moment.
type Consent struct {
	ID             string       `json:"id"`
	PartyID        string       `json:"partyId"`
	Moment         string       `json:"moment"`
	Purpose        string       `json:"purpose"`
	CaptureMethod  string       `json:"captureMethod"`
	CapturedBy     *string      `json:"capturedBy,omitempty"`
	CapturedAt     time.Time    `json:"capturedAt"`
	ArtefactKey    *string      `json:"artefactRef,omitempty"`
	ArtefactDigest *string      `json:"artefactDigest,omitempty"`
	ArtefactType   *string      `json:"artefactType,omitempty"`
	RevokedAt      *time.Time   `json:"revokedAt,omitempty"`
	RevokedReason  *string      `json:"revokedReason,omitempty"`
	State          ConsentState `json:"state"`
}

// ErrNoArtefact is returned when a consent has no stored artefact to serve.
var ErrNoArtefact = errors.New("this consent has no artefact")

func insertConsent(ctx context.Context, tx store.Querier, c Consent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO consents (id, party_id, moment, purpose, capture_method,
		                      captured_by, captured_at, artefact_key, artefact_digest, artefact_type)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		c.ID, c.PartyID, c.Moment, c.Purpose, c.CaptureMethod,
		c.CapturedBy, c.CapturedAt, c.ArtefactKey, c.ArtefactDigest, c.ArtefactType)
	return err
}

func getConsent(ctx context.Context, q store.Querier, consentID string) (Consent, error) {
	row := q.QueryRow(ctx, `
		SELECT id, party_id, moment, purpose, capture_method, captured_by, captured_at,
		       artefact_key, artefact_digest, artefact_type, revoked_at, revoked_reason
		FROM consents WHERE id = $1`, consentID)
	return scanConsent(row)
}

func scanConsent(row store.Row) (Consent, error) {
	var c Consent
	if err := row.Scan(&c.ID, &c.PartyID, &c.Moment, &c.Purpose, &c.CaptureMethod,
		&c.CapturedBy, &c.CapturedAt, &c.ArtefactKey, &c.ArtefactDigest, &c.ArtefactType,
		&c.RevokedAt, &c.RevokedReason); err != nil {
		return Consent{}, err
	}
	c.State = ConsentGranted
	if c.RevokedAt != nil {
		c.State = ConsentWithdrawn
	}
	return c, nil
}

func listConsents(ctx context.Context, q store.Querier, partyID string) ([]Consent, error) {
	rows, err := q.Query(ctx, `
		SELECT id, party_id, moment, purpose, capture_method, captured_by, captured_at,
		       artefact_key, artefact_digest, artefact_type, revoked_at, revoked_reason
		FROM consents WHERE party_id = $1 ORDER BY captured_at`, partyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Consent
	for rows.Next() {
		c, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// enrolmentConsentOf is what resolve reports and what evidence acts on.
//
// A withdrawn consent and a never-granted one are different facts and are kept
// different: "this worker asked us to stop" needs a different answer from "no
// consent was ever recorded", and collapsing them would let a deployment that
// simply never captures consent look identical to one honouring a withdrawal.
func enrolmentConsentOf(ctx context.Context, q store.Querier, partyID string) (ConsentState, error) {
	var live, withdrawn int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE revoked_at IS NULL),
		       count(*) FILTER (WHERE revoked_at IS NOT NULL)
		FROM consents WHERE party_id = $1 AND moment = 'enrolment'`,
		partyID).Scan(&live, &withdrawn); err != nil {
		return ConsentNone, err
	}
	switch {
	case live > 0:
		return ConsentGranted, nil
	case withdrawn > 0:
		return ConsentWithdrawn, nil
	default:
		return ConsentNone, nil
	}
}

func withdrawConsent(ctx context.Context, tx store.Querier, consentID, reason string, at time.Time) (Consent, error) {
	row := tx.QueryRow(ctx, `
		UPDATE consents SET revoked_at = $2, revoked_reason = $3
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id, party_id, moment, purpose, capture_method, captured_by, captured_at,
		          artefact_key, artefact_digest, artefact_type, revoked_at, revoked_reason`,
		consentID, at, reason)
	return scanConsent(row)
}

// clearArtefactKey forgets the key after the artefact itself is gone, so the
// row cannot point at something that is no longer there.
func clearArtefactKey(ctx context.Context, tx store.Querier, consentID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE consents SET artefact_key = NULL, artefact_digest = NULL, artefact_type = NULL
		 WHERE id = $1`, consentID)
	return err
}

// recordConsent captures a consent moment, with its artefact if it has one.
//
// The artefact is uploaded as the request body — audio/ogg, audio/wav — with
// the metadata in the query string, rather than multipart. A field device
// posting a recording over a bad link should not have to assemble a MIME
// document, and the failure mode of multipart on a truncated upload is a parse
// error that names nothing.
//
// The artefact is stored BEFORE the row, deliberately. If the row were written
// first and the upload then failed, the record would claim a recording that
// does not exist. In the other order the worst case is an object with no row
// pointing at it: invisible, sweepable, and nobody's consent is misdescribed.
func (h *handlers) recordConsent(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	q := r.URL.Query()

	c := Consent{
		ID:            id.New(h.d.Clock, "consent"),
		PartyID:       partyID,
		Moment:        q.Get("moment"),
		Purpose:       q.Get("purpose"),
		CaptureMethod: q.Get("captureMethod"),
		CapturedAt:    h.d.Clock.Now(),
	}
	if c.Moment == "" {
		c.Moment = "enrolment"
	}
	if by := q.Get("capturedBy"); by != "" {
		c.CapturedBy = &by
	}
	if c.Purpose == "" || c.CaptureMethod == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request",
			"a consent needs a purpose and a captureMethod; consent to something unstated is not consent")
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), partyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}

	// A body is required for voice and optional otherwise. The database
	// constraint says the same thing; this says it in words a caller can read.
	if r.ContentLength != 0 {
		if h.d.Blobs == nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "no_object_store",
				"this deployment has no object store, so a consent artefact cannot be kept")
			return
		}
		contentType := r.Header.Get("Content-Type")
		blob, err := h.d.Blobs.Put(r.Context(), "consent", r.Body, contentType)
		if errors.Is(err, store.ErrBlobTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "artefact_too_large",
				"the recording is larger than this deployment accepts; it was not stored, "+
					"because a truncated consent recording stops before the part that matters")
			return
		}
		if err != nil {
			httpx.Fail(w, h.d.Log, "store consent artefact", err)
			return
		}
		c.ArtefactKey, c.ArtefactDigest, c.ArtefactType = &blob.Key, &blob.Digest, &blob.ContentType
	} else if c.CaptureMethod == "voice" {
		httpx.WriteError(w, http.StatusBadRequest, "voice_consent_needs_a_recording",
			"a voice consent was declared with no recording attached. For a worker who "+
				"cannot read the form this would be their entire consent record")
		return
	}

	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertConsent(r.Context(), tx, c)
	}); err != nil {
		// The artefact is already stored. Leaving it would be an orphan nobody
		// can find, so it goes back out before the error is reported.
		if c.ArtefactKey != nil && h.d.Blobs != nil {
			if derr := h.d.Blobs.Delete(r.Context(), *c.ArtefactKey); derr != nil {
				h.d.Log.Error("orphaned a consent artefact", "key", *c.ArtefactKey, "error", derr)
			}
		}
		httpx.Fail(w, h.d.Log, "record consent", err)
		return
	}
	c.State = ConsentGranted
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *handlers) listPartyConsents(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	consents, err := listConsents(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list consents", err)
		return
	}
	if consents == nil {
		consents = []Consent{}
	}
	state, err := enrolmentConsentOf(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "derive consent state", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": partyID, "consents": consents, "enrolmentConsent": state,
	})
}

// artefact streams a stored recording back.
//
// The worker's own consent record is something they are entitled to have played
// back to them — a consent you cannot review is one you cannot meaningfully
// withdraw.
func (h *handlers) consentArtefact(w http.ResponseWriter, r *http.Request) {
	c, err := getConsent(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "consent", err, store.ErrNotFound)
		return
	}
	if c.ArtefactKey == nil {
		httpx.WriteError(w, http.StatusNotFound, "no_artefact",
			"this consent has no stored artefact")
		return
	}
	if h.d.Blobs == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_object_store",
			"this deployment has no object store configured")
		return
	}
	body, err := h.d.Blobs.Get(r.Context(), *c.ArtefactKey)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "consent artefact", err, store.ErrNotFound)
		return
	}
	defer func() { _ = body.Close() }()

	if c.ArtefactType != nil {
		w.Header().Set("Content-Type", *c.ArtefactType)
	}
	// Never cached by anything in between. This is a recording of a person.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, body); err != nil {
		h.d.Log.Error("streaming a consent artefact failed", "consent", c.ID, "error", err)
	}
}

// withdraw takes consent back, and means it.
//
// The row stays and is marked; the recording is deleted. Keeping the row is
// what makes the withdrawal auditable — that consent was given and later taken
// back is history, and history is not rewritten. Deleting the artefact is what
// makes it real: a withdrawal that leaves the recording in place has withdrawn
// nothing.
func (h *handlers) withdrawConsent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 && !httpx.ReadJSON(w, r, &body) {
		return
	}

	var c Consent
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		c, err = withdrawConsent(r.Context(), tx, r.PathValue("id"), body.Reason, h.d.Clock.Now())
		return err
	})
	if err != nil {
		// Already withdrawn reads as not-found here, which is the wrong story.
		if existing, gerr := getConsent(r.Context(), h.d.DB.Q(), r.PathValue("id")); gerr == nil &&
			existing.RevokedAt != nil {
			httpx.WriteJSON(w, http.StatusOK, existing)
			return
		}
		httpx.NotFoundOr(w, h.d.Log, "consent", err, store.ErrNotFound)
		return
	}

	if c.ArtefactKey != nil && h.d.Blobs != nil {
		if err := h.d.Blobs.Delete(r.Context(), *c.ArtefactKey); err != nil {
			// The withdrawal is committed; the recording is not gone. That is
			// not something to swallow — somebody has to remove it — so it is
			// logged loudly and the caller is told the truth.
			h.d.Log.Error("a withdrawn consent's artefact was not deleted",
				"consent", c.ID, "key", *c.ArtefactKey, "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "artefact_not_deleted",
				"the consent is withdrawn but its recording could not be deleted; "+
					"this needs an operator, and the key is in the service log")
			return
		}
		if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
			return clearArtefactKey(r.Context(), tx, c.ID)
		}); err != nil {
			httpx.Fail(w, h.d.Log, "clear artefact key", err)
			return
		}
		c.ArtefactKey, c.ArtefactDigest, c.ArtefactType = nil, nil, nil
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}
