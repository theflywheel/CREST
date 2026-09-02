package verification

// Presentation requests: §9's disclosure consent, made an object (w1_15,
// w1_19, w1_20).
//
// The contract, in the blueprint's own words: "who may see what, per
// presentation. Showing the bare QR is itself consent for the non-identifying
// payload; anything more requires an explicit, scoped, recorded grant naming
// who asked and why. Enforced by the verification service, recorded in the
// private consent store."
//
// Three properties are enforced rather than documented:
//
//   - Who is asking, and why, before you decide (w1_19): a request cannot be
//     created without a proven requester and a purpose the worker will read —
//     10–200 characters, the batch-purpose ruling, because it ends up in front
//     of a person who should not need a codebook.
//   - Consent, per share, every time (w1_15): an approval releases exactly one
//     collection. Collecting fulfils the request; asking again is a new
//     request and a new decision. There is no standing grant to mint here.
//   - The worker sees the same list the verifier does (w1_20): both sides of
//     GET /v1/presentation-requests/{id} read one resolved disclosure list,
//     computed by one function, and the fulfilment hands over exactly the ids
//     the worker approved — the handler re-reads the approved list from the
//     record and serves nothing else.
//
// A refusal is a first-class answer with a reason, not an absence: the same
// posture as a declined invitation and a recovery refusal.
//
// What this deliberately is NOT: a new primitive. The Consent primitive's
// disclosure moment (§2, §9) is what happened here; this table is the
// verification service's enforcement record of it, beside the presentation
// trail it already keeps. The parties service's consent ledger has a
// 'disclosure' moment this flow does not yet write into — stated in the PR
// rather than papered over.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// Share-request states. REQUESTED → APPROVED → FULFILLED, or REQUESTED →
// DECLINED, and nothing else. EXPIRED is derived from expiresAt at read time,
// never stored: whether a request is still answerable is a judgement about
// now, and this codebase does not store judgements.
const (
	shareRequested = "REQUESTED"
	shareApproved  = "APPROVED"
	shareDeclined  = "DECLINED"
	shareFulfilled = "FULFILLED"
	shareExpired   = "EXPIRED" // derived only
)

const (
	minSharePurpose = 10
	maxSharePurpose = 200
)

var (
	errShareNeedsPurpose = errors.New("a share request names who is asking and why")
	errSharePurposeSize  = fmt.Errorf("purpose must be %d to %d characters", minSharePurpose, maxSharePurpose)
	errShareSelfRequest  = errors.New("a share request comes from someone else")
	errShareDecided      = errors.New("this share request has already been answered")
	errShareExpiredErr   = errors.New("this share request has expired")
	errShareNotApproved  = errors.New("this share request is not approved")
	errShareNeedsList    = errors.New("an approval names what it approves")
	errShareNotRequested = errors.New("a list can only approve credentials the request put in front of the worker")
	errShareRefuseReason = errors.New("a refusal records a reason")
	errShareCollected    = errors.New("this share has already been collected")
)

// shareRequest is the grant's lifecycle record.
type shareRequest struct {
	ID             string     `json:"id"`
	SubjectPartyID string     `json:"subjectPartyId"`
	RequestedBy    string     `json:"requestedByPartyId"`
	Purpose        string     `json:"purpose"`
	RequestedIDs   []string   `json:"requestedCredentialIds,omitempty"`
	State          string     `json:"state"`
	ApprovedIDs    []string   `json:"approvedCredentialIds,omitempty"`
	DeclineReason  *string    `json:"declineReason,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	DecidedAt      *time.Time `json:"decidedAt,omitempty"`
	FulfilledAt    *time.Time `json:"fulfilledAt,omitempty"`
}

// effectiveState is the state as of now: a REQUESTED or APPROVED request past
// its expiry reads EXPIRED. Derived on every read, stored never.
func (s shareRequest) effectiveState(now time.Time) string {
	if (s.State == shareRequested || s.State == shareApproved) && now.After(s.ExpiresAt) {
		return shareExpired
	}
	return s.State
}

// newShareRequest is the pure half of creation: who is asking, of whom, and
// why — all three present or no request exists.
func newShareRequest(reqID, subject, requestedBy, purpose string,
	requestedIDs []string, now time.Time, ttl time.Duration) (shareRequest, error) {
	if subject == "" || requestedBy == "" {
		return shareRequest{}, errShareNeedsPurpose
	}
	if subject == requestedBy {
		// A worker does not petition themselves; their own wallet view is
		// GET /v1/credentials?partyId=, no consent ceremony attached.
		return shareRequest{}, errShareSelfRequest
	}
	if n := len([]rune(purpose)); n < minSharePurpose || n > maxSharePurpose {
		if purpose == "" {
			return shareRequest{}, errShareNeedsPurpose
		}
		return shareRequest{}, errSharePurposeSize
	}
	return shareRequest{
		ID: reqID, SubjectPartyID: subject, RequestedBy: requestedBy,
		Purpose: purpose, RequestedIDs: requestedIDs, State: shareRequested,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}, nil
}

// decideShare is the pure state machine of the worker's answer.
//
// The approved list must be a subset of the disclosure list the worker was
// shown — the resolved list, not the verifier's raw ask — and may be any
// subset, including empty-but-explicit is not allowed: approving nothing IS
// declining, and the machine refuses the ambiguous middle so a worker's "no"
// is always recorded as a refusal with its reason, never as an empty yes.
func decideShare(s shareRequest, approve bool, approvedIDs []string, reason string,
	disclosure []string, now time.Time) (shareRequest, error) {
	switch s.effectiveState(now) {
	case shareRequested:
	case shareExpired:
		return s, errShareExpiredErr
	default:
		return s, errShareDecided
	}
	at := now
	if !approve {
		if reason == "" {
			return s, errShareRefuseReason
		}
		s.State = shareDeclined
		s.DeclineReason = &reason
		s.DecidedAt = &at
		return s, nil
	}
	if len(approvedIDs) == 0 {
		return s, errShareNeedsList
	}
	shown := map[string]bool{}
	for _, id := range disclosure {
		shown[id] = true
	}
	for _, id := range approvedIDs {
		if !shown[id] {
			return s, errShareNotRequested
		}
	}
	s.State = shareApproved
	s.ApprovedIDs = approvedIDs
	s.DecidedAt = &at
	return s, nil
}

// collectShare is the pure gate on the verifier's pickup: approved, in time,
// and once. Fulfilment consumes the approval — per share, every time.
func collectShare(s shareRequest, now time.Time) (shareRequest, error) {
	switch s.effectiveState(now) {
	case shareApproved:
	case shareFulfilled:
		return s, errShareCollected
	case shareExpired:
		return s, errShareExpiredErr
	default:
		return s, errShareNotApproved
	}
	at := now
	s.State = shareFulfilled
	s.FulfilledAt = &at
	return s, nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

func registerShareRoutes(mux *http.ServeMux, d service.Deps) {
	// How long a request stands open. L2 with an L1 default: a market-day
	// pilot and a national programme can reasonably differ on the number;
	// that a request expires at all is not configurable away.
	ttl, err := config.Duration("CREST_PRESENTATION_REQUEST_TTL", 72*time.Hour)
	if err != nil {
		d.Log.Error("CREST_PRESENTATION_REQUEST_TTL is unreadable", "error", err)
		ttl = 72 * time.Hour
	}
	h := &shareHandlers{d: d, ttl: ttl}
	mux.HandleFunc("POST /v1/presentation-requests", h.create)
	mux.HandleFunc("GET /v1/presentation-requests", h.list)
	mux.HandleFunc("GET /v1/presentation-requests/{id}", h.get)
	mux.HandleFunc("POST /v1/presentation-requests/{id}/decision", h.decide)
	mux.HandleFunc("POST /v1/presentation-requests/{id}/collect", h.collect)
}

type shareHandlers struct {
	d   service.Deps
	ttl time.Duration
}

// disclosedCredential is one row of the disclosure list — the same row on the
// worker's screen and the verifier's (w1_20). Ids and dates only: the list
// says WHICH credentials are in question, and the documents themselves move
// only on an approved collect.
type disclosedCredential struct {
	CredentialID string    `json:"credentialId"`
	IssuedAt     time.Time `json:"issuedAt"`
	Revoked      bool      `json:"revoked"`
}

// disclosureList resolves what a request would share as of now: the subject's
// credentials (across any merge, like every party-scoped read), narrowed to
// the requested ids where the verifier named some. One function, called for
// both faces — that is w1_20, structurally.
func (h *shareHandlers) disclosureList(ctx context.Context, s shareRequest) ([]disclosedCredential, error) {
	ids, err := h.d.SameParty(ctx, s.SubjectPartyID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []string{s.SubjectPartyID}
	}
	rows, err := h.d.DB.Q().Query(ctx, `
		SELECT id, issued_at, revoked_at IS NOT NULL FROM credentials
		 WHERE subject_ref = ANY($1)
		 ORDER BY issued_at, id`, ids)
	if err != nil {
		return nil, err
	}
	all, err := store.Collect(rows, func(r store.Row) (disclosedCredential, error) {
		var c disclosedCredential
		return c, r.Scan(&c.CredentialID, &c.IssuedAt, &c.Revoked)
	})
	if err != nil {
		return nil, err
	}
	if s.RequestedIDs == nil {
		return all, nil
	}
	asked := map[string]bool{}
	for _, id := range s.RequestedIDs {
		asked[id] = true
	}
	out := []disclosedCredential{}
	for _, c := range all {
		if asked[c.CredentialID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (h *shareHandlers) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubjectPartyID string   `json:"subjectPartyId"`
		RequestedBy    string   `json:"requestedByPartyId"`
		Purpose        string   `json:"purpose"`
		CredentialIDs  []string `json:"credentialIds,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	// The requester is the caller (#102): "who is asking" must be a name that
	// was proven, or the screen w1_19 exists for shows a fiction.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.RequestedBy, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	req, err := newShareRequest(id.New(h.d.Clock, "share-request"),
		body.SubjectPartyID, body.RequestedBy, body.Purpose,
		body.CredentialIDs, h.d.Clock.Now(), h.ttl)
	switch {
	case errors.Is(err, errShareSelfRequest):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "self_request",
			"a share request comes from someone else; your own credentials are GET /v1/credentials")
		return
	case errors.Is(err, errShareNeedsPurpose):
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"subjectPartyId, requestedByPartyId and purpose are all required: consent to an "+
				"unnamed asker for an unstated reason is not consent (§9)")
		return
	case errors.Is(err, errSharePurposeSize):
		httpx.WriteError(w, http.StatusBadRequest, "purpose_size",
			"purpose must be %d to %d characters: it is read by the worker, who should not "+
				"need a codebook", minSharePurpose, maxSharePurpose)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "create share request", err)
		return
	}
	if err := h.insert(r.Context(), req); err != nil {
		httpx.Fail(w, h.d.Log, "record share request", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.view(r.Context(), req))
}

// get serves both faces of one request. The caller must be its subject or its
// requester — a share negotiation is between two named parties, not a public
// noticeboard — and both are answered from the same record and the same
// resolved disclosure list.
func (h *shareHandlers) get(w http.ResponseWriter, r *http.Request) {
	req, err := h.read(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "share request", err, store.ErrNotFound)
		return
	}
	if !h.partyToThisShare(w, r, req) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(r.Context(), req))
}

// partyToThisShare admits the subject or the requester, in that order of
// trying, and writes the refusal itself otherwise.
func (h *shareHandlers) partyToThisShare(w http.ResponseWriter, r *http.Request, s shareRequest) bool {
	if !h.d.Authenticating {
		return true
	}
	caller := identity.From(r.Context())
	for _, party := range []string{s.SubjectPartyID, s.RequestedBy} {
		if _, err := identity.Actor(r.Context(), caller, party, "",
			h.d.Authenticating, h.d.Permits); err == nil {
			return true
		}
	}
	h.d.Log.Info("refused a read of a share request by a stranger to it",
		"share", s.ID, "proved", caller.PartyID)
	httpx.WriteError(w, http.StatusForbidden, "not_your_share",
		"this share request is between its subject and its requester")
	return false
}

func (h *shareHandlers) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	subject, verifier := q.Get("subjectPartyId"), q.Get("requestedByPartyId")
	var whose, column string
	switch {
	case subject != "":
		whose, column = subject, "subject_party_id"
	case verifier != "":
		whose, column = verifier, "requested_by"
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"name subjectPartyId or requestedByPartyId: share requests are answered to their "+
				"parties, never browsed")
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, whose, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT id FROM presentation_requests WHERE `+column+` = $1
		ORDER BY created_at, id`, whose)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list share requests", err)
		return
	}
	ids, err := store.Collect(rows, func(r store.Row) (string, error) {
		var v string
		return v, r.Scan(&v)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "list share requests", err)
		return
	}
	out := []map[string]any{}
	for _, reqID := range ids {
		req, err := h.read(r.Context(), h.d.DB.Q(), reqID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "read share request", err)
			return
		}
		out = append(out, h.view(r.Context(), req))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"requests": out, "count": len(out)})
}

// decide is the worker's answer — per share, every time (w1_15).
func (h *shareHandlers) decide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approve               bool     `json:"approve"`
		ApprovedCredentialIDs []string `json:"approvedCredentialIds,omitempty"`
		Reason                string   `json:"reason,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	now := h.d.Clock.Now()
	var req shareRequest
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := h.read(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		req = got
		return nil
	})
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "share request", err, store.ErrNotFound)
		return
	}
	// The decider is the subject — the worker, or someone permitted to act for
	// them; a share of YOUR history is yours to answer (#102, §9).
	if _, ok := identity.Authorize(w, r, h.d.Log, req.SubjectPartyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	disclosure, err := h.disclosureList(r.Context(), req)
	if err != nil {
		httpx.Fail(w, h.d.Log, "resolve disclosure list", err)
		return
	}
	shown := make([]string, 0, len(disclosure))
	for _, c := range disclosure {
		shown = append(shown, c.CredentialID)
	}
	decided, err := decideShare(req, body.Approve, body.ApprovedCredentialIDs, body.Reason, shown, now)
	switch {
	case errors.Is(err, errShareDecided):
		httpx.WriteError(w, http.StatusConflict, "already_answered",
			"this share request has been answered; a settled answer stays settled")
		return
	case errors.Is(err, errShareExpiredErr):
		httpx.WriteError(w, http.StatusConflict, "request_expired",
			"this share request has expired; the verifier asks again and the worker decides again")
		return
	case errors.Is(err, errShareRefuseReason):
		httpx.WriteError(w, http.StatusBadRequest, "refusal_needs_a_reason",
			"declining records why — the refusal is the worker's answer on the record, not an absence")
		return
	case errors.Is(err, errShareNeedsList):
		httpx.WriteError(w, http.StatusBadRequest, "approval_needs_a_list",
			"an approval names the credentials it approves; approving nothing is declining, "+
				"and a decline carries its reason")
		return
	case errors.Is(err, errShareNotRequested):
		httpx.WriteError(w, http.StatusBadRequest, "not_on_the_list",
			"only credentials on this request's disclosure list can be approved — the list the "+
				"worker was shown is the outer bound of what this consent can release")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "decide share request", err)
		return
	}
	if err := h.update(r.Context(), decided); err != nil {
		httpx.Fail(w, h.d.Log, "record share decision", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(r.Context(), decided))
}

// collect hands the verifier exactly what the worker approved — once.
//
// The documents served are re-read from the approved id list on the record,
// through no other filter: the response and the approval cannot disagree
// (w1_20), and a second collect is refused because the approval was for one
// share (w1_15).
func (h *shareHandlers) collect(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	var (
		req  shareRequest
		docs []json.RawMessage
	)
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := h.read(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		req = got
		return nil
	})
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "share request", err, store.ErrNotFound)
		return
	}
	// The collector is the requester the worker consented to — nobody else,
	// however authenticated (#102, §9).
	if _, ok := identity.Authorize(w, r, h.d.Log, req.RequestedBy, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := h.read(r.Context(), tx, req.ID)
		if err != nil {
			return err
		}
		collected, err := collectShare(got, now)
		if err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `
			SELECT doc FROM credentials WHERE id = ANY($1) ORDER BY issued_at, id`,
			collected.ApprovedIDs)
		if err != nil {
			return err
		}
		docs, err = store.Collect(rows, func(r store.Row) (json.RawMessage, error) {
			var doc []byte
			return doc, r.Scan(&doc)
		})
		if err != nil {
			return err
		}
		// Every shared credential writes its own presentation entry: "who
		// checked me" reads a consented share like any other check (§9, W8).
		for _, doc := range docs {
			var cred struct {
				ID      string `json:"id"`
				Subject struct {
					ID string `json:"id"`
				} `json:"credentialSubject"`
			}
			_ = json.Unmarshal(doc, &cred)
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO presentations (id, credential_id, subject_ref, requested_by, purpose,
				                           scope, outcome, tier, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				id.New(h.d.Clock, "presentation"), nullable(cred.ID), nullable(cred.Subject.ID),
				nullable(collected.RequestedBy), nullable(collected.Purpose),
				"consented", "shared", nil, now); err != nil {
				return err
			}
		}
		req = collected
		return h.write(r.Context(), tx, collected)
	})
	switch {
	case errors.Is(err, errShareNotApproved):
		httpx.WriteError(w, http.StatusConflict, "not_approved",
			"the worker has not approved this share; there is nothing to collect")
		return
	case errors.Is(err, errShareCollected):
		httpx.WriteError(w, http.StatusConflict, "already_collected",
			"this share was already collected: consent here is per share, every time — ask again")
		return
	case errors.Is(err, errShareExpiredErr):
		httpx.WriteError(w, http.StatusConflict, "request_expired",
			"this approval has expired uncollected; ask again")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "collect share", err)
		return
	}
	if docs == nil {
		docs = []json.RawMessage{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"request": req, "credentials": docs, "count": len(docs),
	})
}

// view is a request plus its resolved disclosure list and derived state — the
// one shape both faces read.
func (h *shareHandlers) view(ctx context.Context, s shareRequest) map[string]any {
	now := h.d.Clock.Now()
	out := map[string]any{"request": s, "state": s.effectiveState(now)}
	disclosure, err := h.disclosureList(ctx, s)
	if err != nil {
		// The list could not be resolved (the registry is down). Said plainly
		// rather than served empty: an empty list reads as "nothing would be
		// shared", which is a different and false statement.
		out["disclosureList"] = nil
		out["disclosureListError"] = "the disclosure list could not be resolved right now"
		return out
	}
	out["disclosureList"] = disclosure
	return out
}

// ── Store ───────────────────────────────────────────────────────────────────

func (h *shareHandlers) insert(ctx context.Context, s shareRequest) error {
	return h.d.DB.InTx(ctx, func(tx store.Querier) error {
		requested, err := jsonOrNull(s.RequestedIDs)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO presentation_requests
			  (id, subject_party_id, requested_by, purpose, requested_credential_ids,
			   state, created_at, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			s.ID, s.SubjectPartyID, s.RequestedBy, s.Purpose, requested,
			s.State, s.CreatedAt, s.ExpiresAt)
		return err
	})
}

func (h *shareHandlers) update(ctx context.Context, s shareRequest) error {
	return h.d.DB.InTx(ctx, func(tx store.Querier) error {
		return h.write(ctx, tx, s)
	})
}

func (h *shareHandlers) write(ctx context.Context, tx store.Querier, s shareRequest) error {
	approved, err := jsonOrNull(s.ApprovedIDs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE presentation_requests
		SET state = $2, approved_credential_ids = $3, decline_reason = $4,
		    decided_at = $5, fulfilled_at = $6
		WHERE id = $1`,
		s.ID, s.State, approved, s.DeclineReason, s.DecidedAt, s.FulfilledAt)
	return err
}

func (h *shareHandlers) read(ctx context.Context, q store.Querier, reqID string) (shareRequest, error) {
	var s shareRequest
	var requested, approved []byte
	err := q.QueryRow(ctx, `
		SELECT id, subject_party_id, requested_by, purpose, requested_credential_ids,
		       state, approved_credential_ids, decline_reason,
		       created_at, expires_at, decided_at, fulfilled_at
		FROM presentation_requests WHERE id = $1`, reqID).Scan(
		&s.ID, &s.SubjectPartyID, &s.RequestedBy, &s.Purpose, &requested,
		&s.State, &approved, &s.DeclineReason,
		&s.CreatedAt, &s.ExpiresAt, &s.DecidedAt, &s.FulfilledAt)
	if err != nil {
		return shareRequest{}, err
	}
	if requested != nil {
		if err := json.Unmarshal(requested, &s.RequestedIDs); err != nil {
			return shareRequest{}, err
		}
	}
	if approved != nil {
		if err := json.Unmarshal(approved, &s.ApprovedIDs); err != nil {
			return shareRequest{}, err
		}
	}
	return s, nil
}

func jsonOrNull(ids []string) (any, error) {
	if ids == nil {
		return nil, nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return b, nil
}
