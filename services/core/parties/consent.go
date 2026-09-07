package parties

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/idempotency"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
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
	ContextID      *string      `json:"contextId,omitempty"`
	CapturedBy     *string      `json:"capturedBy,omitempty"`
	CapturedAt     time.Time    `json:"capturedAt"`
	ArtefactKey    *string      `json:"artefactRef,omitempty"`
	ArtefactDigest *string      `json:"artefactDigest,omitempty"`
	ArtefactType   *string      `json:"artefactType,omitempty"`
	RevokedAt      *time.Time   `json:"revokedAt,omitempty"`
	RevokedReason  *string      `json:"revokedReason,omitempty"`
	State          ConsentState `json:"state"`
}

// validCaptureMethods mirrors 0005's CHECK constraint. Two copies of one list
// is a thing to keep honest, and the alternative — letting the database be the
// only check — turns a typo into a 500.
var validCaptureMethods = map[string]bool{
	"screen": true, "sms": true, "ussd": true, "voice": true, "assisted": true,
}

// ErrNoArtefact is returned when a consent has no stored artefact to serve.
var ErrNoArtefact = errors.New("this consent has no artefact")

var errNoConsentObjectStore = errors.New("consent object store is unavailable")

var errLiveEnrolmentConsent = errors.New("an enrolment consent is already live for this programme")

func insertConsent(ctx context.Context, tx store.Querier, c Consent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO consents (id, party_id, moment, purpose, capture_method, context_id,
		                      captured_by, captured_at, artefact_key, artefact_digest, artefact_type)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		c.ID, c.PartyID, c.Moment, c.Purpose, c.CaptureMethod, c.ContextID,
		c.CapturedBy, c.CapturedAt, c.ArtefactKey, c.ArtefactDigest, c.ArtefactType)
	return err
}

func getConsent(ctx context.Context, q store.Querier, consentID string) (Consent, error) {
	row := q.QueryRow(ctx, `
		SELECT id, party_id, moment, purpose, capture_method, context_id, captured_by, captured_at,
		       artefact_key, artefact_digest, artefact_type, revoked_at, revoked_reason
		FROM consents WHERE id = $1`, consentID)
	return scanConsent(row)
}

func scanConsent(row store.Row) (Consent, error) {
	var c Consent
	if err := row.Scan(&c.ID, &c.PartyID, &c.Moment, &c.Purpose, &c.CaptureMethod, &c.ContextID,
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
		SELECT id, party_id, moment, purpose, capture_method, context_id, captured_by, captured_at,
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

// enrolmentConsentOf is what resolve reports and what evidence acts on, for one
// party in one context.
//
// Scoped, because consent is per relationship. A worker who agreed to one
// programme holding their work history has not thereby agreed to every other
// organisation on the same deployment holding it — nobody asked them that.
// Asking again when they join a second programme is asking a second question,
// which is what it is.
//
// A withdrawn consent and a never-granted one are different facts and are kept
// different: "this worker asked us to stop" needs a different answer from "no
// consent was ever recorded", and collapsing them would let a deployment that
// simply never captures consent look identical to one honouring a withdrawal.
func enrolmentConsentOf(ctx context.Context, q store.Querier, partyID, contextID string) (ConsentState, error) {
	var live, withdrawn int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE revoked_at IS NULL),
		       count(*) FILTER (WHERE revoked_at IS NOT NULL)
		FROM consents WHERE party_id = $1 AND moment = 'enrolment' AND context_id = $2`,
		partyID, contextID).Scan(&live, &withdrawn); err != nil {
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
		RETURNING id, party_id, moment, purpose, capture_method, context_id, captured_by, captured_at,
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
// The artefact is uploaded before the row, deliberately, while the SQL
// transaction is still open. If the row were written first and the upload then
// failed, the record would claim a recording that does not exist. Every failure
// visible to this process removes the newly minted object before returning;
// the store interface intentionally mints opaque keys, so deployments that
// require crash level orphan reclamation must provide an object-store sweep.
func (h *handlers) recordConsent(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	q := r.URL.Query()
	// Recording a consent is acting in the worker's name (#102): the worker
	// themselves, or the agent capturing it, who must be permitted to act for
	// them in the consent's context.
	if _, ok := identity.Authorize(w, r, h.d.Log, partyID, q.Get("contextId"),
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	caller := identity.From(r.Context())
	if caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "caller_required", "a consent capture must have an authenticated agent")
		return
	}
	key, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	raw, ok := readConsentBody(w, r)
	if !ok {
		return
	}

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
		if by != caller.PartyID {
			httpx.WriteError(w, http.StatusForbidden, "actor_mismatch", "capturedBy must be the authenticated caller")
			return
		}
	}
	c.CapturedBy = &caller.PartyID
	if c.Purpose == "" || c.CaptureMethod == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request",
			"a consent needs a purpose and a captureMethod; consent to something unstated is not consent")
		return
	}
	// The allowed set lived only in 0005's CHECK constraint, so an unrecognised
	// method reached the database and came back as a bare 500 naming a
	// constraint. The caller could not tell a typo from an outage, which is the
	// same failure the context check fixed on the evidence side.
	if !validCaptureMethods[c.CaptureMethod] {
		httpx.WriteError(w, http.StatusBadRequest, "unknown_capture_method",
			"captureMethod %q is not one this deployment records; it must be one of "+
				"screen, sms, ussd, voice or assisted", c.CaptureMethod)
		return
	}
	if ctxID := q.Get("contextId"); ctxID != "" {
		c.ContextID = &ctxID
	} else if c.Moment == "enrolment" {
		httpx.WriteError(w, http.StatusBadRequest, "enrolment_consent_needs_a_context",
			"an enrolment consent names the programme it is given to. An unscoped one would "+
				"mean this worker agreed to every organisation on this deployment holding "+
				"their history, which nobody asked them")
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), partyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}

	// A body is required for voice and optional otherwise. The database
	// constraint says the same thing; this says it in words a caller can read.
	if len(raw) == 0 && c.CaptureMethod == "voice" {
		httpx.WriteError(w, http.StatusBadRequest, "voice_consent_needs_a_recording",
			"a voice consent was declared with no recording attached. For a worker who "+
				"cannot read the form this would be their entire consent record")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if c.CaptureMethod == "voice" && !validVoiceRecording(contentType, raw) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_voice_recording",
			"voice consent requires an Ogg, WAV, WebM, or MP4 audio recording whose bytes have the matching audio signature")
		return
	}
	// The programme has to exist. Without this the failure is a foreign-key
	// violation surfacing as a bare 500 — which is what the first PoC run got,
	// fifteen times, saying nothing about which of the two ids was wrong.
	if c.ContextID != nil {
		var exists bool
		if err := h.d.DB.Q().QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM contexts WHERE id = $1)`, *c.ContextID).Scan(&exists); err != nil {
			httpx.Fail(w, h.d.Log, "check context", err)
			return
		}
		if !exists {
			if c.ArtefactKey != nil && h.d.Blobs != nil {
				if derr := h.d.Blobs.Delete(r.Context(), *c.ArtefactKey); derr != nil {
					h.d.Log.Error("orphaned a consent artefact", "key", *c.ArtefactKey, "error", derr)
				}
			}
			httpx.WriteError(w, http.StatusBadRequest, "no_such_context",
				"there is no programme %q on this deployment, so a consent cannot be scoped to it",
				*c.ContextID)
			return
		}
	}

	var prepared store.PreparedBlobs
	var objectKey string
	if len(raw) > 0 {
		var ok bool
		prepared, ok = h.d.Blobs.(store.PreparedBlobs)
		if !ok {
			httpx.WriteError(w, http.StatusServiceUnavailable, "no_durable_uploads", "object storage does not support recoverable uploads")
			return
		}
		var err error
		objectKey, err = prepareConsentUpload(r.Context(), h.d, idempotency.BodyDigest([]byte(caller.PartyID+"|"+key+"|"+idempotency.CanonicalPath(r)+"|"+idempotency.BodyDigest(raw))))
		if err != nil {
			httpx.Fail(w, h.d.Log, "journal consent upload", err)
			return
		}
	}

	var replay bool
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		reservation, err := beginIdempotency(r.Context(), tx, r, key, caller.PartyID, raw)
		if err != nil {
			return err
		}
		if reservation.Replay() {
			if reservation.Result().ResourceType != "consent" || reservation.Result().ResourceID == "" {
				return errors.New("idempotency replay has no consent resource")
			}
			c.ID = reservation.Result().ResourceID
			replay = true
			return nil
		}
		// Check the one-live-consent invariant before touching object storage.
		// A distinct retry key must not upload a second recording and then turn
		// the database's partial-index violation into a 500. The row lock protects
		// an existing consent; the unique index below handles concurrent inserts
		// when this precheck finds no row.
		if c.Moment == "enrolment" && c.ContextID != nil {
			var existingID string
			err := tx.QueryRow(r.Context(), `
				SELECT id FROM consents
				WHERE party_id = $1 AND context_id = $2 AND moment = 'enrolment' AND revoked_at IS NULL
				FOR UPDATE`, c.PartyID, *c.ContextID).Scan(&existingID)
			switch {
			case err == nil:
				return errLiveEnrolmentConsent
			case !errors.Is(err, store.ErrNotFound):
				return err
			}
		}
		// Keep the request reservation, artefact row and completion marker in
		// one transaction. Uploading while this transaction is open lets all
		// known failure paths delete the newly created object before returning.
		if len(raw) > 0 {
			if h.d.Blobs == nil {
				return errNoConsentObjectStore
			}
			var locked string
			if err := tx.QueryRow(r.Context(), "SELECT object_key FROM consent_upload_intents WHERE object_key=$1 FOR UPDATE", objectKey).Scan(&locked); err != nil {
				return err
			}
			blob, err := prepared.PutPrepared(r.Context(), objectKey, bytes.NewReader(raw), contentType)
			if errors.Is(err, store.ErrBlobTooLarge) {
				return err
			}
			if err != nil {
				return err
			}
			c.ArtefactKey, c.ArtefactDigest, c.ArtefactType = &blob.Key, &blob.Digest, &blob.ContentType
		}
		if err := insertConsent(r.Context(), tx, c); err != nil {
			return err
		}
		return reservation.Complete(r.Context(), tx, idempotency.Result{
			Status: http.StatusCreated, ResourceType: "consent", ResourceID: c.ID,
		})
	}); err != nil {
		if errors.Is(err, errLiveEnrolmentConsent) {
			discardConsentUpload(r.Context(), h.d, objectKey)
			httpx.WriteError(w, http.StatusConflict, "enrolment_consent_already_live",
				"this worker already has live enrolment consent for this programme; the existing consent remains in effect")
			return
		}
		if isLiveEnrolmentUniqueViolation(err) && c.Moment == "enrolment" && c.ContextID != nil {
			// A concurrent insert can still win between the lookup and the
			// insert under a different transaction snapshot. Confirm the live
			// row before translating the unique violation so unrelated duplicate
			// errors are not mislabeled.
			state, stateErr := enrolmentConsentOf(r.Context(), h.d.DB.Q(), c.PartyID, *c.ContextID)
			if stateErr == nil && state == ConsentGranted {
				discardConsentUpload(r.Context(), h.d, objectKey)
				httpx.WriteError(w, http.StatusConflict, "enrolment_consent_already_live",
					"this worker already has live enrolment consent for this programme; the existing consent remains in effect")
				return
			}
		}
		if errors.Is(err, idempotency.ErrFingerprint) || errors.Is(err, idempotency.ErrInProgress) {
			writeIdempotencyError(w, h.d.Log, err)
			return
		}
		if errors.Is(err, store.ErrBlobTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "artefact_too_large",
				"the recording is larger than this deployment accepts; it was not stored")
			return
		}
		if errors.Is(err, errNoConsentObjectStore) {
			httpx.WriteError(w, http.StatusServiceUnavailable, "no_object_store",
				"this deployment has no object store, so a consent artefact cannot be kept")
			return
		}
		// The durable intent is reclaimed only after proving no consent refers to it.
		// A lost COMMIT response must never cause deletion of a committed recording.

		httpx.Fail(w, h.d.Log, "record consent", err)
		return
	}
	if replay {
		stored, err := getConsent(r.Context(), h.d.DB.Q(), c.ID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "reconstruct consent", err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, stored)
		return
	}
	c.State = ConsentGranted
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func isLiveEnrolmentUniqueViolation(err error) bool {
	// The store package deliberately keeps the PostgreSQL driver behind its
	// interface. Match the named invariant after the generic SQLSTATE check so
	// another unique constraint is not reported as a live-consent conflict.
	return store.IsUniqueViolation(err) && strings.Contains(err.Error(), "consents_one_live_enrolment_per_context")
}

// discardConsentUpload removes an upload prepared for a transaction that did
// not commit. The intent is deleted after the object removal; if cleanup itself
// fails, the durable intent remains for recoverConsentUploads to retry safely.
func discardConsentUpload(ctx context.Context, d service.Deps, objectKey string) {
	if objectKey == "" {
		return
	}
	if d.Blobs != nil {
		if err := d.Blobs.Delete(ctx, objectKey); err != nil {
			d.Log.Error("discarding uncommitted consent artefact failed", "key", objectKey, "error", err)
			return
		}
	}
	if _, err := d.DB.Q().Exec(ctx, "DELETE FROM consent_upload_intents WHERE object_key=$1", objectKey); err != nil {
		d.Log.Error("discarding consent upload intent failed", "key", objectKey, "error", err)
	}
}

func (h *handlers) listPartyConsents(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	// The worker's own consent history (#102).
	if _, ok := identity.Authorize(w, r, h.d.Log, partyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	consents, err := listConsents(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list consents", err)
		return
	}
	if consents == nil {
		consents = []Consent{}
	}
	// One state per programme, because there is no single answer any more:
	// a worker can be consented to one and withdrawn from another, and
	// flattening that would hide exactly the distinction this scoping adds.
	states := map[string]ConsentState{}
	for _, c := range consents {
		if c.Moment != "enrolment" || c.ContextID == nil {
			continue
		}
		state, err := enrolmentConsentOf(r.Context(), h.d.DB.Q(), partyID, *c.ContextID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "derive consent state", err)
			return
		}
		states[*c.ContextID] = state
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": partyID, "consents": consents, "enrolmentConsent": states,
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
	// The artefact is somebody's voice. The party comes from the record, not
	// the request (#102).
	if _, ok := identity.Authorize(w, r, h.d.Log, c.PartyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
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

	// Whose consent this is has to be established before it can be withdrawn
	// (#89). This is the endpoint the issue names first, and for the reason
	// that makes it worst: withdrawing enrolment consent stops new evidence
	// being recorded about a worker (§9), so one unauthenticated call made
	// somebody's work stop counting — and the endpoint whose entire purpose is
	// acting on a worker's behalf had nothing establishing on whose behalf.
	//
	// The read is outside the transaction and the withdrawal re-reads inside
	// it. Reading here only to find out who to check against is not a race
	// worth a lock: a consent's party never changes, and the transaction below
	// is still the one that decides whether there is anything to withdraw.
	existing, err := getConsent(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "consent", err, store.ErrNotFound)
		return
	}
	contextID := ""
	if existing.ContextID != nil {
		contextID = *existing.ContextID
	}
	// The assisted case reaches here intact: a supervisor withdrawing for a
	// worker who cannot operate a phone sends X-CREST-On-Behalf-Of and holds
	// act-for-party in the project, and the withdrawal is theirs to make.
	if _, ok := identity.Authorize(w, r, h.d.Log, existing.PartyID, contextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}

	var c Consent
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
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

// enrolmentConsentState answers "may we record new evidence about this worker on
// this programme?" for a party somebody has named, rather than one a resolve
// found.
//
// /v1/resolve already derives this, but only as a side effect of matching an
// identifier. Working the unclear queue is the case where the worker is named
// outright — the whole point is that no identifier matched — so there is
// nothing to resolve, and without this the caller would have to list every
// consent the worker holds and re-derive the rule itself. A second
// implementation of "is this worker withdrawn" is a second thing to get wrong,
// and the one that gets it wrong records evidence about somebody who asked us
// to stop (§9).
//
// 404 when the party does not exist, so a caller cannot re-attribute work to an
// identifier it invented.
func (h *handlers) enrolmentConsentState(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	contextID := r.URL.Query().Get("contextId")
	// Public on /v1 for the worker and their actors; evidence asks the
	// /internal twin per matched row (#102).
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		if _, ok := identity.Authorize(w, r, h.d.Log, partyID, contextID,
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	}
	if contextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"contextId is required: consent is per programme, so there is no single answer "+
				"about a worker without naming one (§9)")
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), partyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}
	state, err := enrolmentConsentOf(r.Context(), h.d.DB.Q(), partyID, contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "derive consent state", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": partyID, "contextId": contextID, "enrolmentConsent": state,
	})
}
