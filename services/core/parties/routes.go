package parties

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
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

	// The caller-facing surface (#102). Party-scoped reads and every write in
	// a party's name go through identity.Authorize: the worker themselves, or
	// somebody holding act-for-party where the action is scoped. Endpoints the
	// other services call have /internal/ twins below — unauthenticated by the
	// service-identity ruling (§16, decided 2026-08-25): service traffic lives
	// on internal routes and fencing them to the service network is the
	// deployment's job, stated in pkg/identity/remote.go.
	//
	// POST /v1/parties is deliberately open: it is the bootstrap and
	// self-registration door (the first party cannot be created by an
	// authenticated party), and the checked door for field enrolment is
	// /v1/enrolments, which names its enroller. Revisit when org-admin
	// identity lands (J1); #102 tracks the judgement.
	mux.HandleFunc("POST /v1/parties", h.createParty)
	// The eSignet login surface (#155): served only where a relying-party
	// client is configured; the local stack's mock-oidc has no redirect flow.
	if a := loadAuthConfig(d.Log); a != nil {
		registerAuth(mux, d, a)
	}
	mux.HandleFunc("GET /v1/parties/{id}", h.getParty)
	mux.HandleFunc("GET /v1/parties/{id}/assurance", h.getAssurance)
	mux.HandleFunc("POST /v1/parties/{id}/roster-ids", h.addRosterID)
	mux.HandleFunc("POST /v1/parties/{id}/identity-bindings", h.addIdentityBinding)
	// Service twins: contact routes are read here, evidence resolves identities
	// and consent state, verification derives assurance mid-verdict.
	mux.HandleFunc("GET /internal/parties/{id}", h.getPartyRaw)
	mux.HandleFunc("GET /internal/parties/{id}/assurance", h.getAssurance)
	mux.HandleFunc("GET /internal/parties/{id}/enrolment-consent", h.enrolmentConsentState)
	mux.HandleFunc("GET /internal/resolve", h.resolve)
	mux.HandleFunc("GET /internal/authorizations/permits", h.permits)
	mux.HandleFunc("GET /internal/authorizations", h.listAuthorizationsRaw)

	// Enrolment consent (§9, #24). The artefact route is separate from the
	// record route because one is a stream of somebody's voice and the other
	// is metadata, and they should never be served by the same handler.
	mux.HandleFunc("POST /v1/parties/{id}/consents", h.recordConsent)
	mux.HandleFunc("GET /v1/parties/{id}/consents", h.listPartyConsents)
	mux.HandleFunc("GET /v1/parties/{id}/enrolment-consent", h.enrolmentConsentState)
	mux.HandleFunc("GET /v1/consents/{id}/artefact", h.consentArtefact)
	mux.HandleFunc("POST /v1/consents/{id}/withdraw", h.withdrawConsent)
	mux.HandleFunc("GET /v1/resolve", h.resolve)
	mux.HandleFunc("GET /v1/holds", h.listHolds)
	// Closing a hold, not only listing it (§4, W7). See holds.go.
	mux.HandleFunc("POST /v1/holds/{id}/resolve", h.resolveHold)
	mux.HandleFunc("GET /v1/holds/metrics", h.mergeMetrics)
	// g4_4: coverage-by-place. See coverage.go.
	mux.HandleFunc("GET /v1/coverage", h.coverage)
	// g4_5: the registry quality worklist. See worklist.go.
	mux.HandleFunc("GET /v1/quality-worklist", h.qualityWorklist)
	mux.HandleFunc("POST /v1/terms", h.createTerms)
	mux.HandleFunc("GET /v1/terms", h.listTerms)
	mux.HandleFunc("POST /v1/authorizations", h.createAuthorization)
	mux.HandleFunc("GET /v1/authorizations/permits", h.permits)
	mux.HandleFunc("GET /v1/authorizations/mine", h.myAuthorizations)
	mux.HandleFunc("GET /v1/authorizations", h.listAuthorizations)
	mux.HandleFunc("GET /v1/authorizations/overdue", h.overdueAuthorizations)
	mux.HandleFunc("GET /v1/authorizations/{id}", h.readAuthorization)
	mux.HandleFunc("POST /v1/authorizations/{id}/revoke", h.revokeAuthorization)
	mux.HandleFunc("POST /v1/contexts", h.createContext)

	// The project surface (J3). A project is a Context, so these routes are a
	// face over the primitive rather than a second store of the same thing.
	registerProjectRoutes(mux, d)

	// The G-2 onboarding journey's own surface (§15 J1): project→org
	// invitations — the offer whose acceptance creates a partner grant — and
	// terms-upgrade requests with the check verdicts recorded before a wider
	// set goes live.
	registerOnboardingJourneyRoutes(mux, d)

	// Onboarding (#20). Organisations apply for themselves; workers are often
	// enrolled by someone else, and the two paths are deliberately different
	// shapes rather than one endpoint with a flag.
	mux.HandleFunc("POST /v1/organisations", h.registerOrganisation)
	mux.HandleFunc("GET /v1/organisations/{id}/registration", h.getRegistration)
	mux.HandleFunc("GET /v1/registrations", h.listRegistrations)
	mux.HandleFunc("POST /v1/organisations/{id}/terms-acceptance", h.acceptTerms)
	mux.HandleFunc("POST /v1/organisations/{id}/decision", h.decideRegistration)
	mux.HandleFunc("POST /v1/enrolments", h.assistedEnrolment)
	mux.HandleFunc("GET /v1/parties/{id}/enrolment", h.getEnrolment)

	// Turning a verified token's subject into a Party (#89), and which party
	// ids are one person after a merge (#100). See identity.go.
	registerIdentityRoutes(mux, d)
	registerMergeRoutes(mux, d)
	registerRecoveryRoutes(mux, d)
	registerRecoveryContactRoutes(mux, d)

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
	// A Party document is contact routes and identity bindings — PII. The
	// public read answers the person, or somebody permitted to act for them;
	// services read the /internal twin (#102).
	if _, ok := identity.Authorize(w, r, h.d.Log, r.PathValue("id"), "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	h.getPartyRaw(w, r)
}

func (h *handlers) getPartyRaw(w http.ResponseWriter, r *http.Request) {
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
	// Public on /v1 for the person and their actors; verification asks the
	// /internal twin mid-verdict (#102). The level itself is derived, so this
	// endpoint is a window, not a store.
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		if _, ok := identity.Authorize(w, r, h.d.Log, r.PathValue("id"), "",
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	}
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
	// A roster id is a joining identifier: it decides whose work a future CSV
	// row becomes (#102). Registering one is acting in the worker's name, in
	// the roster's own context.
	if _, ok := identity.Authorize(w, r, h.d.Log, r.PathValue("id"), body.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
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
	// Resolution maps a joining identifier to a person — the custodian's
	// "find a worker" and the evidence pipeline's attribution step. On /v1 it
	// answers signed-in callers only: anonymous identifier probing is how a
	// phone number becomes a party id (#102). Evidence uses the /internal
	// twin.
	if strings.HasPrefix(r.URL.Path, "/v1/") &&
		!identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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
	//
	// A resolve with no contextId reports NONE, because consent is per
	// programme and there is no programme to answer about. NONE is the
	// permissive value, which is safe here only because the evidence pipeline
	// always resolves within the context of the batch it is ingesting — a
	// caller that omitted it could not act on the answer anyway.
	consent, err := enrolmentConsentOf(r.Context(), h.d.DB.Q(), match.PartyID, contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "derive consent state", err)
		return
	}
	match.EnrolmentConsent = consent
	httpx.WriteJSON(w, http.StatusOK, match)
}

func (h *handlers) listHolds(w http.ResponseWriter, r *http.Request) {
	// A custodian queue. It shows existence rather than content — key values
	// are deliberately not serialised — so any signed-in caller may read it;
	// deciding one is the authorized act (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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

// listTerms answers what an applicant would be agreeing to. Public, like the
// terms themselves — they are published to the registry log (§3).
func (h *handlers) listTerms(w http.ResponseWriter, r *http.Request) {
	terms, err := listTerms(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list terms", err)
		return
	}
	if terms == nil {
		terms = []schema.Terms{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"terms": terms, "count": len(terms)})
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
	// A grant decides who may act for whom, and a valid token is not, by
	// itself, authority to decide that: any signed-in worker could otherwise
	// mint themselves act-for-party and pass every later permits() check
	// (#124 review). The caller must prove they ARE — or act for — the
	// authority the grant names, because authorityPartyId is "who says so"
	// and this request is that party saying so.
	ctxID := ""
	if a.Scope.ContextID != nil {
		ctxID = *a.Scope.ContextID
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, a.AuthorityPartyID, ctxID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	// approvedByPartyId stays a recorded fact rather than a cross-checked
	// claim: the fixture world already holds a grant approved by a custodian
	// person under the organisation's authority, and requiring it to equal
	// the acting party would make that history unloadable. The gate above is
	// the one that matters — whoever the record names as approver, only the
	// authority (or somebody proven to act for it) can put the record here.
	// And the authority must be an organisation — the same line
	// listAuthorizations draws. A person naming themselves as their own
	// authority is the self-mint above wearing different field names.
	authority, err := getParty(r.Context(), h.d.DB.Q(), a.AuthorityPartyID)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "authority party", err, store.ErrNotFound)
		return
	}
	if authority.Kind != schema.PartyKindOrganisation {
		httpx.WriteError(w, http.StatusForbidden, "not_an_organisation",
			"authorityPartyId must name an organisation; a person cannot be their own authority")
		return
	}
	// And an APPROVED one. Party kind alone is not authority: POST /v1/parties
	// is the open bootstrap door, so anyone can mint an organisation-shaped
	// party and first-bind to it (#124 review). What makes an organisation an
	// authority is the registry's decision — application, terms, and an
	// approval with somebody's name on it — so the gate reads the registration
	// rather than the shape.
	reg, err := getRegistration(r.Context(), h.d.DB.Q(), a.AuthorityPartyID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, h.d.Log, "read authority registration", err)
		return
	}
	if err != nil || reg.State != stateApproved {
		httpx.WriteError(w, http.StatusForbidden, "organisation_not_approved",
			"authorityPartyId must name an APPROVED organisation; a party of the right shape "+
				"is not an authority until the registry's decision says so")
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
		// The insert is an upsert so a reseed can replay the same grant, but
		// an id is a public fact, not a proof, and a grant is not editable: it
		// is narrowed by revoke and re-grant, never rewritten in place. So a
		// duplicate id is accepted only as an exact replay — same authority
		// and the same document, byte for byte (#124 review). Anything less
		// would let the upsert rewrite doc, state and review_by while the
		// indexed permission columns keep the old row's values, a grant whose
		// document says one thing while permits() enforces another.
		existing, err := getAuthorizationLocked(r.Context(), tx, a.ID, true)
		switch {
		case err == nil:
			if existing.AuthorityPartyID != a.AuthorityPartyID {
				return errGrantIDTaken
			}
			prev, errPrev := json.Marshal(existing)
			cur, errCur := json.Marshal(a)
			if errPrev != nil || errCur != nil {
				return cmp.Or(errPrev, errCur)
			}
			if !bytes.Equal(prev, cur) {
				return errGrantNotEditable
			}
		case errors.Is(err, store.ErrNotFound):
			// New id; nothing to protect.
		default:
			return err
		}
		if err := insertAuthorization(r.Context(), tx, a); err != nil {
			return err
		}
		// Enqueued for every authorization; the delivery path decides whether
		// it may be published. A worker's authorization is refused there, with
		// the reason, rather than being filtered out silently here — see the
		// design finding in publish.go.
		return enqueueFact(r.Context(), tx, "authorization", a.ID, 1)
	}); err != nil {
		if errors.Is(err, errGrantIDTaken) {
			httpx.WriteError(w, http.StatusConflict, "grant_id_taken",
				"an authorization with this id already stands under a different authority")
			return
		}
		if errors.Is(err, errGrantNotEditable) {
			httpx.WriteError(w, http.StatusConflict, "grant_not_editable",
				"an authorization is narrowed by revoke and re-grant, not edited in place; "+
					"a duplicate id is accepted only as an exact replay")
			return
		}
		httpx.Fail(w, h.d.Log, "create authorization", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

// errGrantIDTaken is the create upsert refusing to let one authority's grant
// be replaced through another authority's gate.
var errGrantIDTaken = errors.New("authorization id belongs to a different authority")

// errGrantNotEditable is a same-authority duplicate id that is not an exact
// replay — an attempted in-place edit of a grant.
var errGrantNotEditable = errors.New("authorization exists and differs; grants are not edited in place")

// readAuthorization returns one grant, to its own authority and nobody else.
// The seeder uses it to compare a standing grant against the fixture's shape
// rather than trusting the permits predicate, which deliberately answers yes
// for an instance-scoped grant in any context and so cannot detect scope
// drift. Restricted to the authority because a grant read by id names a
// subject and their functions — for a person, that is a record of who works
// where (#68), and the authority is the one party that already knows it.
func (h *handlers) readAuthorization(w http.ResponseWriter, r *http.Request) {
	a, err := getAuthorization(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "authorization", err, store.ErrNotFound)
		return
	}
	ctxID := ""
	if a.Scope.ContextID != nil {
		ctxID = *a.Scope.ContextID
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, a.AuthorityPartyID, ctxID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

// listAuthorizations answers "which authorization stands behind this?" for one
// party at one scope (#16).
//
// Scoped queries only, and organisations only. An unscoped listing, or one that
// answered for a person, would be a roster query — and a roster of who works
// where is precisely what #68 established must not be readable, whether from
// the log or from here.
// myAuthorizations is the self-read #68's refusal deliberately left open: a
// signed-in person listing THEIR OWN active grants. What #68 forbids is a
// third party assembling who-works-where; a person's own list is the opposite
// — it is how a console shows somebody the roles they actually hold instead
// of offering them a role to pick.
func (h *handlers) myAuthorizations(w http.ResponseWriter, r *http.Request) {
	caller := identity.From(r.Context())
	if !caller.Authenticated() || caller.PartyID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "no_token",
			"this endpoint answers about a verified caller's own grants")
		return
	}
	list, err := activeAuthorizationsHeldBy(r.Context(), h.d.DB.Q(), caller.PartyID, h.d.Clock.Now())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list own authorizations", err)
		return
	}
	if list == nil {
		list = []schema.Authorization{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": caller.PartyID, "authorizations": list, "count": len(list),
	})
}

func (h *handlers) listAuthorizations(w http.ResponseWriter, r *http.Request) {
	// A custodian/operations read: who holds what, where. Signed-in callers
	// only (#102); the anonymous question this could answer is a roster probe.
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	h.listAuthorizationsBody(w, r)
}

// listAuthorizationsRaw is the service twin (§16). Confirmation calls it while
// issuing, to name the organisation's authorization a verifier walks up to.
//
// It needs its own route rather than a token because that read happens on the
// path that releases payment, and it is best-effort there: a refusal does not
// fail the issuance, it silently drops qualificationRef and grantRef and the
// credential goes out with a chain that stops at an org id. That is the worst
// shape a failure can take — a credential that looks issued and cannot be
// walked — and it is what closing the caller-facing route without opening this
// one produced.
func (h *handlers) listAuthorizationsRaw(w http.ResponseWriter, r *http.Request) {
	h.listAuthorizationsBody(w, r)
}

func (h *handlers) listAuthorizationsBody(w http.ResponseWriter, r *http.Request) {
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
	// A boolean about somebody's permissions is still a fact about them —
	// probing it unauthenticated is a membership oracle (#68). Signed-in
	// callers on /v1; services ask the /internal twin (#102).
	if strings.HasPrefix(r.URL.Path, "/v1/") &&
		!identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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
	ok, overdue, err := permits(r.Context(), h.d.DB.Q(), q.Get("partyId"), q.Get("function"), q.Get("contextId"), at)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check authorization", err)
		return
	}
	// overdue never changes permitted (§16): it says the authorization behind
	// this yes is past its review-by date, for a caller that wants to surface
	// staleness. A caller that ignores it loses nothing a worker needed.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"permitted": ok, "overdue": overdue, "at": at})
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

// overdueAuthorizations is the review queue §16 asked for: every ACTIVE
// authorization past its review-by date. Listing it is the entire consequence
// of overdueness — the ruling is "flag for review, keep working", so nothing
// reads this list to refuse anything. Like /v1/holds, this is a custodian
// surface; the authorization pass over the whole API is #102.
func (h *handlers) overdueAuthorizations(w http.ResponseWriter, r *http.Request) {
	// The review queue is a custodian surface like /v1/holds: signed-in
	// callers only (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	list, err := overdueAuthorizations(r.Context(), h.d.DB.Q(), h.d.Clock.Now())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list overdue authorizations", err)
		return
	}
	if list == nil {
		list = []schema.Authorization{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"authorizations": list, "count": len(list)})
}

// revokeAuthorization narrows a grant after go-live (§16, J3). The ruling it
// implements: narrowing binds at submission. Revocation stops NEW work — the
// next permits() check answers no — and touches nothing in flight, because
// evidence already submitted was checked when it entered and the worker cannot
// un-perform the work. To narrow rather than remove, revoke and create the
// narrower authorization; there is deliberately no edit, because an edited
// grant has no record of what it used to allow.
func (h *handlers) revokeAuthorization(w http.ResponseWriter, r *http.Request) {
	// Revocation changes who may act from the next permits() answer onward,
	// so it is gated like creation: the caller must prove they are — or act
	// for — the authority whose grant this is. Any signed-in subject who
	// learned a grant id could otherwise switch off another party's ability
	// to submit (#124 review).
	grant, err := getAuthorization(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "authorization", err, store.ErrNotFound)
		return
	}
	ctxID := ""
	if grant.Scope.ContextID != nil {
		ctxID = *grant.Scope.ContextID
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, grant.AuthorityPartyID, ctxID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	var out schema.Authorization
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		a, err := revokeAuthorization(r.Context(), tx, r.PathValue("id"), h.d.Clock.Now())
		if err != nil {
			return err
		}
		out = a
		return enqueueFact(r.Context(), tx, "authorization", a.ID, 1)
	})
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "authorization", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func writeValidation(w http.ResponseWriter, err error) {
	var ve *schema.ValidationError
	if errors.As(err, &ve) {
		httpx.WriteProblems(w, "schema_violation", "the document does not satisfy "+ve.SchemaID, ve.Problems)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "validation failed: %v", err)
}
