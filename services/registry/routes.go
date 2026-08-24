package main

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func routes(mux *http.ServeMux, d service.Deps) {
	model, err := loadApprovalModel()
	if err != nil {
		// Fatal rather than defaulted. A deployment that meant to require a
		// human approver and silently got automatic approval is a deployment
		// where nobody's name is on any of it.
		d.Log.Error("organisation approval model is unusable", "error", err)
		os.Exit(1)
	}
	h := &handlers{d: d, model: model}

	mux.HandleFunc("POST /v1/parties", h.createParty)
	mux.HandleFunc("GET /v1/parties/{id}", h.getParty)
	mux.HandleFunc("GET /v1/parties/{id}/assurance", h.getAssurance)
	mux.HandleFunc("POST /v1/parties/{id}/roster-ids", h.addRosterID)
	mux.HandleFunc("POST /v1/parties/{id}/identity-bindings", h.addIdentityBinding)

	// Enrolment consent (§9, #24). The artefact route is separate from the
	// record route because one is a stream of somebody's voice and the other
	// is metadata, and they should never be served by the same handler.
	mux.HandleFunc("POST /v1/parties/{id}/consents", h.recordConsent)
	mux.HandleFunc("GET /v1/parties/{id}/consents", h.listPartyConsents)
	mux.HandleFunc("GET /v1/consents/{id}/artefact", h.consentArtefact)
	mux.HandleFunc("POST /v1/consents/{id}/withdraw", h.withdrawConsent)
	mux.HandleFunc("GET /v1/resolve", h.resolve)
	mux.HandleFunc("GET /v1/holds", h.listHolds)
	mux.HandleFunc("POST /v1/terms", h.createTerms)
	mux.HandleFunc("POST /v1/authorizations", h.createAuthorization)
	mux.HandleFunc("GET /v1/authorizations/permits", h.permits)
	mux.HandleFunc("GET /v1/authorizations", h.listAuthorizations)
	mux.HandleFunc("POST /v1/contexts", h.createContext)

	// Onboarding (#20). Organisations apply for themselves; workers are often
	// enrolled by someone else, and the two paths are deliberately different
	// shapes rather than one endpoint with a flag.
	mux.HandleFunc("POST /v1/organisations", h.registerOrganisation)
	mux.HandleFunc("GET /v1/organisations/{id}/registration", h.getRegistration)
	mux.HandleFunc("POST /v1/organisations/{id}/terms-acceptance", h.acceptTerms)
	mux.HandleFunc("POST /v1/organisations/{id}/decision", h.decideRegistration)
	mux.HandleFunc("POST /v1/enrolments", h.assistedEnrolment)
	mux.HandleFunc("GET /v1/parties/{id}/enrolment", h.getEnrolment)

	// Where a public fact landed on the registry substrate (§3).
	mux.HandleFunc("GET /v1/publications/{kind}/{id}", h.publication)

	// The deployment's public self-description (#70). Unauthenticated: one
	// nobody outside can read is one nobody outside can check.
	mux.HandleFunc("GET /v1/instance", h.getInstance)

	// The skill list (#16). Reference data rather than a primitive — §3 files
	// it beside credential shapes and adapters.
	mux.HandleFunc("POST /v1/skills", h.publishSkill)
	mux.HandleFunc("GET /v1/skills", h.listSkills)
	mux.HandleFunc("GET /v1/skills/{code}", h.getSkill)
}

type handlers struct {
	d     service.Deps
	model approvalModel
}

func (h *handlers) createParty(w http.ResponseWriter, r *http.Request) {
	var p schema.Party
	if !httpx.ReadJSON(w, r, &p) {
		return
	}
	if p.ID == "" {
		p.ID = id.Party(h.d.Clock)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = h.d.Clock.Now()
	}
	// Validated against the schema rather than trusted because it unmarshalled.
	// The struct cannot express "at least one contact route", and W2 is
	// unenforceable against a Party nobody can reach.
	if err := schema.Validate(schema.IDParty, p); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertParty(r.Context(), tx, p)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create party", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, p)
}

func (h *handlers) getParty(w http.ResponseWriter, r *http.Request) {
	p, err := getParty(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

// getAssurance derives the level rather than reading it, for the same reason
// the tier is derived: a stored level freezes a judgement, and cannot be
// upgraded when a worker binds an anchor later (§4.1, and the same mechanism as
// retroactive credentialing).
func (h *handlers) getAssurance(w http.ResponseWriter, r *http.Request) {
	p, err := getParty(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}
	level, because := assuranceOf(p, h.d.Clock.Now())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId":           p.ID,
		"identityAssurance": level,
		"because":           because,
	})
}

func (h *handlers) addRosterID(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RosterID  string `json:"rosterId"`
		ContextID string `json:"contextId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.RosterID == "" || body.ContextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a roster id needs the context it is scoped to; an unscoped one matches the wrong person")
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return putRosterID(r.Context(), tx, r.PathValue("id"), body.RosterID, body.ContextID)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "add roster id", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolve is the endpoint the evidence pipeline calls for every row.
//
// Three answers, and the third is the point of the design:
//
//	200  exactly one candidate — with which key matched and at what confidence
//	404  nothing matched — the caller sends it to the unclear queue
//	409  more than one candidate — a hold is recorded, and no merge happens
func (h *handlers) resolve(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	value := r.URL.Query().Get("value")
	contextID := r.URL.Query().Get("contextId")
	if value == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "value is required")
		return
	}

	match, candidates, err := resolve(r.Context(), h.d.DB.Q(), kind, value, contextID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "no_match",
			"no party matches that identifier; this row belongs in the unclear queue")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "resolve", err)
		return
	}

	if len(candidates) > 0 {
		hold := Hold{
			ID:         id.New(h.d.Clock, "match-hold"),
			KeyKind:    kind,
			KeyValue:   value,
			Candidates: candidates,
			Reason:     "more than one party carries this identifier",
			CreatedAt:  h.d.Clock.Now(),
		}
		if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
			return insertHold(r.Context(), tx, hold)
		}); err != nil {
			httpx.Fail(w, h.d.Log, "record hold", err)
			return
		}
		// 409 rather than picking the best candidate. Picking is a merge, and
		// merges_without_confirmation is a monitored metric, not an aspiration.
		httpx.WriteJSON(w, http.StatusConflict, hold)
		return
	}
	// Derived here rather than joined in, so the answer cannot be stale for the
	// same reason the assurance level is derived: a cached "consented" is a
	// worker's withdrawal that has not taken effect yet.
	consent, err := enrolmentConsentOf(r.Context(), h.d.DB.Q(), match.PartyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "derive consent state", err)
		return
	}
	match.EnrolmentConsent = consent
	httpx.WriteJSON(w, http.StatusOK, match)
}

func (h *handlers) listHolds(w http.ResponseWriter, r *http.Request) {
	holds, err := openHolds(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list holds", err)
		return
	}
	if holds == nil {
		holds = []Hold{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"holds": holds, "count": len(holds)})
}

func (h *handlers) createTerms(w http.ResponseWriter, r *http.Request) {
	var t schema.Terms
	if !httpx.ReadJSON(w, r, &t) {
		return
	}
	if t.PublishedAt.IsZero() {
		t.PublishedAt = h.d.Clock.Now()
	}
	if err := schema.Validate(schema.IDTerms, t); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertTerms(r.Context(), tx, t); err != nil {
			return err
		}
		// Terms are a public fact (§3). Enqueued in the same transaction as
		// the row, so a crash cannot leave terms that exist here and nowhere a
		// verifier can reach.
		return enqueueFact(r.Context(), tx, "terms", t.ID, t.Version)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create terms", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (h *handlers) createAuthorization(w http.ResponseWriter, r *http.Request) {
	var a schema.Authorization
	if !httpx.ReadJSON(w, r, &a) {
		return
	}
	if a.ID == "" {
		a.ID = id.New(h.d.Clock, "authorization")
	}
	if a.ApprovedAt.IsZero() {
		a.ApprovedAt = h.d.Clock.Now()
	}
	if a.State == "" {
		a.State = schema.AuthorizationStateACTIVE
	}
	if err := schema.Validate(schema.IDAuthorization, a); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertAuthorization(r.Context(), tx, a); err != nil {
			return err
		}
		// Enqueued for every authorization; the delivery path decides whether
		// it may be published. A worker's authorization is refused there, with
		// the reason, rather than being filtered out silently here — see the
		// design finding in publish.go.
		return enqueueFact(r.Context(), tx, "authorization", a.ID, 1)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create authorization", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

// listAuthorizations answers "which authorization stands behind this?" for one
// party at one scope (#16).
//
// Scoped queries only, and organisations only. An unscoped listing, or one that
// answered for a person, would be a roster query — and a roster of who works
// where is precisely what #68 established must not be readable, whether from
// the log or from here.
func (h *handlers) listAuthorizations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	partyID, scopeKind := q.Get("partyId"), q.Get("scope")
	if partyID == "" || scopeKind == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query",
			"partyId and scope are both required; an unscoped listing is a roster query")
		return
	}
	if scopeKind != "instance" && scopeKind != "context" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "scope is instance or context")
		return
	}
	party, err := getParty(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}
	if party.Kind != schema.PartyKindOrganisation {
		// Refused rather than filtered to empty. A caller that received an
		// empty list would conclude the person holds no authorizations, which
		// is a different and false statement.
		httpx.WriteError(w, http.StatusForbidden, "not_an_organisation",
			"authorizations are listable for organisations only; a person's would be a record of who works where (#68)")
		return
	}

	at := h.d.Clock.Now()
	if s := q.Get("at"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "at is not RFC3339: %v", err)
			return
		}
		at = parsed
	}
	list, err := authorizationsFor(r.Context(), h.d.DB.Q(), partyID, scopeKind, q.Get("contextId"), at)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list authorizations", err)
		return
	}
	if list == nil {
		list = []schema.Authorization{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"authorizations": list, "count": len(list)})
}

func (h *handlers) permits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	at := h.d.Clock.Now()
	if s := q.Get("at"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "at is not RFC3339: %v", err)
			return
		}
		at = parsed
	}
	ok, err := permits(r.Context(), h.d.DB.Q(), q.Get("partyId"), q.Get("function"), q.Get("contextId"), at)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check authorization", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"permitted": ok, "at": at})
}

func (h *handlers) createContext(w http.ResponseWriter, r *http.Request) {
	var c schema.Context
	if !httpx.ReadJSON(w, r, &c) {
		return
	}
	if c.ID == "" {
		c.ID = id.New(h.d.Clock, "context")
	}
	if err := schema.Validate(schema.IDContext, c); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertContext(r.Context(), tx, c)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create context", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func writeValidation(w http.ResponseWriter, err error) {
	var ve *schema.ValidationError
	if errors.As(err, &ve) {
		httpx.WriteProblems(w, "schema_violation", "the document does not satisfy "+ve.SchemaID, ve.Problems)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "validation failed: %v", err)
}
