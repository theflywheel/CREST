package parties

import (
	"errors"
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

// The terms-upgrade request handlers (g2_6–g2_8, g2_11, g2_12). The pure state
// machine lives in termsrequests.go; everything here is the doors: who may
// open, edit, submit, withdraw, check and decide.

// requestBody is what opens a request or replaces a draft's documents.
type requestBody struct {
	TermsID      string             `json:"termsId"`
	TermsVersion int                `json:"termsVersion"`
	Documents    []declaredDocument `json:"documents"`
}

// createTermsRequest opens a DRAFT for an APPROVED organisation.
//
// APPROVED only, deliberately: registration-time terms go through
// POST .../terms-acceptance and the registration decision (g2_11's own
// screen), and this door is the one thing that path cannot do — move an
// already-approved organisation to wider terms, with review in between
// (g2_6). An organisation still in flight edits its application instead.
func (h *g2Handlers) createTermsRequest(w http.ResponseWriter, r *http.Request) {
	var body requestBody
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	orgID := r.PathValue("id")
	actor, ok := identity.Authorize(w, r, h.d.Log, orgID, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	if _, ok := h.requireApprovedOrganisation(w, r, orgID, "a terms request"); !ok {
		return
	}
	// The requested terms must be a published version that exists. A request
	// for terms nobody published is a request for nothing.
	if _, err := getTerms(r.Context(), h.d.DB.Q(), body.TermsID, body.TermsVersion); err != nil {
		if body.TermsID == "" || body.TermsVersion < 1 || errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(w, http.StatusUnprocessableEntity, "no_such_terms",
				"a terms request names a published terms version; this one does not exist")
			return
		}
		httpx.Fail(w, h.d.Log, "read requested terms", err)
		return
	}
	req, ev, err := newTermsRequest(id.New(h.d.Clock, "terms-request"), orgID,
		body.TermsID, body.TermsVersion, body.Documents, cmpOr(actor, orgID), h.d.Clock.Now())
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_request", "%v", err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := upsertTermsRequest(r.Context(), tx, req); err != nil {
			return err
		}
		return appendTermsRequestEvent(r.Context(), tx, req.ID, ev)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create terms request", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, req)
}

func (h *g2Handlers) listOrganisationTermsRequests(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if _, ok := identity.Authorize(w, r, h.d.Log, orgID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	list, err := listTermsRequests(r.Context(), h.d.DB.Q(), orgID, r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list terms requests", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"requests": list, "count": len(list)})
}

// listTermsRequestQueue is the reviewer's read — the queue g2_8's "sent for
// review" lands in. Authenticated like the partner directory: who reviews is
// L2 routing this service does not encode, and nothing here is identity data —
// documents are declared references, never content. state defaults to
// SUBMITTED because the queue is the question this read answers.
func (h *g2Handlers) listTermsRequestQueue(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = requestSubmitted
	}
	list, err := listTermsRequests(r.Context(), h.d.DB.Q(), "", state)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list terms request queue", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"requests": list, "count": len(list)})
}

// getTermsRequest serves one request whole: state, documents, trail and check
// verdicts — g2_8's status view and g2_12's "what is checked" on one read.
func (h *g2Handlers) getTermsRequest(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	req, err := getTermsRequest(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "terms request", err, store.ErrNotFound)
		return
	}
	events, err := termsRequestEvents(r.Context(), h.d.DB.Q(), req.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read terms request trail", err)
		return
	}
	checks, err := checkVerdicts(r.Context(), h.d.DB.Q(), req.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read check verdicts", err)
		return
	}
	if events == nil {
		events = []termsRequestEvent{}
	}
	if checks == nil {
		checks = []checkVerdict{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"request": req, "events": events, "checks": checks,
	})
}

// mutateRequest is the one lock-read-apply-write path for the organisation's
// own transitions. The org (or somebody proven to act for it) is the actor.
func (h *g2Handlers) mutateRequest(w http.ResponseWriter, r *http.Request,
	apply func(termsRequest, string) (termsRequest, *termsRequestEvent, error)) {

	req, err := getTermsRequest(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "terms request", err, store.ErrNotFound)
		return
	}
	actor, ok := identity.Authorize(w, r, h.d.Log, req.PartyID, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	var out termsRequest
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getTermsRequest(r.Context(), tx, req.ID, true)
		if err != nil {
			return err
		}
		next, ev, err := apply(fresh, cmpOr(actor, req.PartyID))
		if err != nil {
			return err
		}
		if err := upsertTermsRequest(r.Context(), tx, next); err != nil {
			return err
		}
		out = next
		if ev == nil {
			return nil
		}
		return appendTermsRequestEvent(r.Context(), tx, next.ID, *ev)
	})
	switch {
	case errors.Is(err, errRequestNotDraft), errors.Is(err, errRequestNotSubmitted),
		errors.Is(err, errRequestDecided):
		httpx.WriteError(w, http.StatusConflict, "wrong_state", "%v", err)
		return
	case err != nil:
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_request", "%v", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// replaceRequestDocuments is g2_7's "Save draft".
func (h *g2Handlers) replaceRequestDocuments(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Documents []declaredDocument `json:"documents"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	h.mutateRequest(w, r, func(req termsRequest, _ string) (termsRequest, *termsRequestEvent, error) {
		next, err := replaceDocuments(req, body.Documents)
		return next, nil, err
	})
}

// submitTermsRequest is g2_7's "Submit" — the moment the declared documents
// freeze and the request enters the review queue (g2_8).
func (h *g2Handlers) submitTermsRequest(w http.ResponseWriter, r *http.Request) {
	h.mutateRequest(w, r, func(req termsRequest, actor string) (termsRequest, *termsRequestEvent, error) {
		next, ev, err := submitTermsRequest(req, actor, h.d.Clock.Now())
		return next, &ev, err
	})
}

// withdrawTermsRequest is g2_8's "Withdraw".
func (h *g2Handlers) withdrawTermsRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	h.mutateRequest(w, r, func(req termsRequest, actor string) (termsRequest, *termsRequestEvent, error) {
		next, ev, err := withdrawTermsRequest(req, actor, body.Reason, h.d.Clock.Now())
		return next, &ev, err
	})
}

// recordCheck writes one verdict onto a submitted request (g2_12). The
// recorder is proven; the owner — a party, or a named policy such as the
// eventual business-register adapter — is who answers for the verdict. No
// automated checker exists in this codebase, and this endpoint is the honest
// shape of that fact: verdicts are recorded, never simulated.
func (h *g2Handlers) recordCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		Outcome    string `json:"outcome"`
		OwnerKind  string `json:"ownerKind"`
		Owner      string `json:"owner"`
		Note       string `json:"note"`
		RecordedBy string `json:"recordedBy"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.RecordedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"recordedBy is required; a verdict nobody recorded is one nobody can be asked about")
		return
	}
	recordedBy, ok := identity.Authorize(w, r, h.d.Log, body.RecordedBy, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	if body.OwnerKind == checkOwnerParty && body.Owner == "" {
		body.Owner = cmpOr(recordedBy, body.RecordedBy)
	}
	req, err := getTermsRequest(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "terms request", err, store.ErrNotFound)
		return
	}
	v, err := newCheckVerdict(req, body.Name, body.Outcome, body.OwnerKind, body.Owner,
		body.Note, cmpOr(recordedBy, body.RecordedBy), h.d.Clock.Now())
	if err != nil {
		if errors.Is(err, errRequestNotSubmitted) {
			httpx.WriteError(w, http.StatusConflict, "wrong_state", "%v", err)
			return
		}
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_check", "%v", err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return appendCheckVerdict(r.Context(), tx, req.ID, v)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record check", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (h *g2Handlers) listChecks(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	checks, err := checkVerdicts(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list check verdicts", err)
		return
	}
	if checks == nil {
		checks = []checkVerdict{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"checks": checks, "count": len(checks)})
}

// decideTermsRequest approves or denies, with the same posture as the
// registration decision: the decider is named and proven, is never the
// applicant, and a denial carries a reason. On approval the organisation's
// registration moves to the requested terms in the same transaction, and the
// terms it held until that moment are captured on the request — the
// registration row only ever shows the current answer.
//
// Whether the recorded checks are sufficient is the decider's judgement under
// L2 policy; this endpoint does not count PASSes, because which checks a
// deployment requires is exactly the kind of rule two deployments disagree on.
func (h *g2Handlers) decideTermsRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approve   bool   `json:"approve"`
		DecidedBy string `json:"decidedBy"`
		Reason    string `json:"reason"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.DecidedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"decidedBy is required; an approval nobody granted is one nobody can be asked about")
		return
	}
	decidedBy, ok := identity.Authorize(w, r, h.d.Log, body.DecidedBy, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	var out termsRequest
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getTermsRequest(r.Context(), tx, r.PathValue("id"), true)
		if err != nil {
			return err
		}
		next, ev, err := decideTermsRequest(fresh, body.Approve,
			cmpOr(decidedBy, body.DecidedBy), body.Reason, h.d.Clock.Now())
		if err != nil {
			return err
		}
		if body.Approve {
			reg, err := getRegistration(r.Context(), tx, next.PartyID)
			if err != nil {
				return err
			}
			// Capture the outgoing terms before the overwrite.
			next.PreviousTermsID = reg.TermsID
			next.PreviousTermsVersion = reg.TermsVersion
			if err := applyApprovedTerms(r.Context(), tx, next); err != nil {
				return err
			}
		}
		if err := upsertTermsRequest(r.Context(), tx, next); err != nil {
			return err
		}
		out = next
		return appendTermsRequestEvent(r.Context(), tx, next.ID, ev)
	})
	switch {
	case errors.Is(err, errRequestSelfDecided):
		httpx.WriteError(w, http.StatusConflict, "self_approved", "%v", err)
		return
	case errors.Is(err, errDenialNeedsReason):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "%v", err)
		return
	case errors.Is(err, errRequestDecided), errors.Is(err, errRequestNotSubmitted):
		httpx.WriteError(w, http.StatusConflict, "wrong_state", "%v", err)
		return
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no such terms request, or the organisation's registration is no longer APPROVED")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "decide terms request", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
