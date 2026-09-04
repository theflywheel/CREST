package parties

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The project surface (J3, screens p1_1–p1_3 and p2_1–p2_21).
//
// Every route here is a face over the Context primitive and the Authorization
// primitive. Nothing in this file invents a lifecycle, a role name, a posture
// or a code format, and the one primitive change behind it — ownership
// acknowledgement — is a design finding recorded in the blueprint rather than
// a field added quietly to make a screen work.
func registerProjectRoutes(mux *http.ServeMux, d service.Deps) {
	h := &projectHandlers{d: d}

	// p1_3 creates one; p1_1 and n2 list them; n4 reads the one that was
	// handed over.
	mux.HandleFunc("POST /v1/projects", h.createProject)
	mux.HandleFunc("GET /v1/projects", h.listProjects)
	mux.HandleFunc("GET /v1/projects/{id}", h.getProject)

	// The handover, both sides of it (design finding F2, screen n4).
	mux.HandleFunc("POST /v1/projects/{id}/configurator", h.nameConfigurator)
	mux.HandleFunc("POST /v1/projects/{id}/ownership-decision", h.decideOwnership)
	mux.HandleFunc("GET /v1/projects/{id}/ownership", h.readOwnership)

	// p2_7: what a project looks like before it runs.
	mux.HandleFunc("GET /v1/projects/{id}/activation", h.readActivation)
	mux.HandleFunc("POST /v1/projects/{id}/activation", h.activateProject)
	mux.HandleFunc("PUT /v1/projects/{id}/activation/gates", h.declareGates)
	mux.HandleFunc("POST /v1/projects/{id}/activation/gates/{name}/satisfied", h.satisfyGate)

	// p1_2, p2_6, n2, n5: a role is held, not just recorded.
	mux.HandleFunc("POST /v1/projects/{id}/roles", h.grantProjectRole)
	mux.HandleFunc("GET /v1/projects/{id}/roles", h.listProjectRoles)
	mux.HandleFunc("GET /v1/organisations/{id}/roles", h.listOrganisationRoles)

	// p2_1, p2_3, p2_5: the composition choices, as a record with a decider.
	mux.HandleFunc("PUT /v1/projects/{id}/composition/{choice}", h.putComposition)
	mux.HandleFunc("GET /v1/projects/{id}/composition", h.readComposition)

	// p2_17, p2_18: the directory registration was for, and a grant narrower
	// than the terms that ends by itself.
	mux.HandleFunc("GET /v1/partners", h.partnerDirectory)
	mux.HandleFunc("POST /v1/projects/{id}/partner-grants", h.grantPartner)
	mux.HandleFunc("GET /v1/projects/{id}/partner-grants", h.listPartnerGrants)

	// p2_8, p2_10: the link and the owner, never the code and never the
	// support desk.
	mux.HandleFunc("PUT /v1/projects/{id}/finance-link", h.putFinanceLink)
	mux.HandleFunc("GET /v1/projects/{id}/finance-link", h.readFinanceLink)
	mux.HandleFunc("PUT /v1/projects/{id}/support-owner", h.putSupportOwner)
	mux.HandleFunc("GET /v1/projects/{id}/support-owner", h.readSupportOwner)
}

type projectHandlers struct {
	d service.Deps
}

// projectView is what every read returns: the primitive, plus the derived
// answers a screen needs and must never re-derive for itself.
type projectView struct {
	schema.Context
	Conditions []activationCondition `json:"activationConditions"`
	Records    []contextRecord       `json:"records,omitempty"`
}

func (h *projectHandlers) view(c schema.Context, records []contextRecord) projectView {
	return projectView{Context: c, Conditions: activationConditions(c), Records: records}
}

// requireApprovedOrganisation is the same gate createAuthorization applies to
// an authority, applied here to a project's owning organisation and to a
// partner being granted anything. POST /v1/parties is the open bootstrap door,
// so organisation shape proves nothing; what makes an organisation real is the
// registry's own decision, with somebody's name on it.
func (h *projectHandlers) requireApprovedOrganisation(w http.ResponseWriter, r *http.Request,
	partyID, what string) (schema.Party, bool) {

	party, err := getParty(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, what, err, store.ErrNotFound)
		return schema.Party{}, false
	}
	if party.Kind != schema.PartyKindOrganisation {
		httpx.WriteError(w, http.StatusForbidden, "not_an_organisation",
			"%s must name an organisation", what)
		return schema.Party{}, false
	}
	reg, err := getRegistration(r.Context(), h.d.DB.Q(), partyID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, h.d.Log, "read registration", err)
		return schema.Party{}, false
	}
	if err != nil || reg.State != stateApproved {
		httpx.WriteError(w, http.StatusForbidden, "organisation_not_approved",
			"%s must name an APPROVED organisation; a party of the right shape is not one "+
				"until the registry's decision says so", what)
		return schema.Party{}, false
	}
	return party, true
}

// project loads one and refuses a context of another kind through this door.
func (h *projectHandlers) project(w http.ResponseWriter, r *http.Request) (schema.Context, bool) {
	c, err := getContext(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "project", err, store.ErrNotFound)
		return schema.Context{}, false
	}
	return c, true
}

// viewProject proves the caller may READ this project, and returns the party
// they are acting as.
//
// Two ways in, and the second is the handover's whole point: the owning
// organisation (or somebody holding act-for-party for it in this context), or
// the named configurator themselves — who, at the moment they are deciding
// whether to accept, may hold no grant at all. Reading n4 must not require the
// grant that accepting it is a precondition for, so the read admits a named
// configurator at any acknowledgement state, a decline included: somebody who
// refused a handover is still entitled to see what they refused.
func (h *projectHandlers) viewProject(w http.ResponseWriter, r *http.Request,
	c schema.Context) (string, bool) {

	return h.gateProject(w, r, c, false)
}

// configureProject proves the caller may WRITE to this project.
//
// The same two ways in, with one difference that is the whole reason
// acknowledgement exists: a named configurator may write only once they have
// ACCEPTED. Naming is a proposal, and a proposal must not carry authority —
// otherwise the acknowledgement is decoration. Concretely, without this a
// party who was named and never answered, or who explicitly DECLINED, could
// still set the project's finance link and support owner, rewrite its
// activation gates, mark them satisfied, and grant a partner organisation a
// standing role on the project. Every one of those writes would outlive the
// refusal, because a re-handover replaces the current ownership view and
// cannot un-grant what the previous nominee did.
//
// The owning organisation is unaffected: it is the authority, and it can write
// to its own project whether anybody has accepted it or not.
func (h *projectHandlers) configureProject(w http.ResponseWriter, r *http.Request,
	c schema.Context) (string, bool) {

	return h.gateProject(w, r, c, true)
}

func (h *projectHandlers) gateProject(w http.ResponseWriter, r *http.Request,
	c schema.Context, write bool) (string, bool) {

	caller := identity.From(r.Context())
	named := isNamedConfigurator(c, caller.PartyID)
	if named && (!write || acknowledgedConfigurator(c, caller.PartyID)) {
		return caller.PartyID, true
	}
	// A person granted a role scoped to this project may READ it. The grant is
	// a recorded relationship — an authority named them into this context — and
	// the facts a project read returns (its composition choices, its declared
	// vocabulary, its activation state) are exactly the facts their role runs
	// on. Without this, a definition author cannot read the vocabulary their
	// own wizard is supposed to offer. Writes are untouched: configuring stays
	// with the owner and the acknowledged configurator.
	if !write && caller.PartyID != "" {
		held, err := activeAuthorizationsHeldBy(r.Context(), h.d.DB.Q(), caller.PartyID, h.d.Clock.Now())
		if err == nil && grantAdmitsRead(held, c.ID) {
			return caller.PartyID, true
		}
	}
	// A named configurator who has not accepted is told what is missing rather
	// than being handed a bare refusal: the thing standing in their way is one
	// button on the screen they are already looking at.
	if write && named {
		h.d.Log.Info("refused a project write from an unacknowledged configurator",
			"path", r.URL.Path, "project", c.ID, "ownership", c.Ownership.State)
		httpx.WriteError(w, http.StatusConflict, "handover_not_accepted",
			"you were named this project's configurator but have not accepted the handover; "+
				"accept it first — naming is a proposal, and a project nobody agreed to run "+
				"must not be configurable by the person who has not agreed")
		return "", false
	}
	return identity.Authorize(w, r, h.d.Log, c.OwnerPartyID, c.ID,
		h.d.Authenticating, h.d.Permits)
}

// createProject is p1_3: a name, a coverage area, an owner and a named
// configurator. Creating a project is not configuring one, and this endpoint
// is deliberately unable to do the second thing — everything that decides how
// the project runs arrives through a later route with a decider recorded
// against it.
func (h *projectHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Owner string `json:"ownerPartyId"`
		// ConfiguratorPartyID is a proposal. It becomes an assignment only when
		// the named party accepts (design finding F2).
		ConfiguratorPartyID string         `json:"configuratorPartyId"`
		ParentID            string         `json:"parentId"`
		Period              *schema.Period `json:"period"`
		// Configuration is L2 and opaque: coverage lives here, and so does
		// anything else this deployment's profile wants on a project. The core
		// stores it and never reads inside it.
		Configuration map[string]any `json:"configuration"`
		// Gates are the deployment's own readiness conditions, by name.
		Gates []string `json:"activationGates"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" || body.Owner == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a project needs a name and an owning organisation")
		return
	}
	// Creating a project in an organisation's name is acting for it. No context
	// scope yet — the project being created is the context, and an authority
	// scoped to it would be circular.
	actor, ok := identity.Authorize(w, r, h.d.Log, body.Owner, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	if _, ok := h.requireApprovedOrganisation(w, r, body.Owner, "ownerPartyId"); !ok {
		return
	}

	now := h.d.Clock.Now()
	c := schema.Context{
		ID:            body.ID,
		Kind:          cmpOr(strings.TrimSpace(body.Kind), contextKindProject),
		Name:          strings.TrimSpace(body.Name),
		OwnerPartyID:  body.Owner,
		Period:        schema.Period{Start: now},
		Configuration: body.Configuration,
		State:         schema.ContextStateDRAFT,
	}
	if c.ID == "" {
		c.ID = id.New(h.d.Clock, "context")
	}
	if body.ParentID != "" {
		c.ParentID = &body.ParentID
	}
	if body.Period != nil {
		c.Period = *body.Period
	}
	var err error
	if c, err = declareGates(c, body.Gates, now); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "%v", err)
		return
	}

	var ev ownershipEvent
	named := body.ConfiguratorPartyID != ""
	if named {
		if _, err := getParty(r.Context(), h.d.DB.Q(), body.ConfiguratorPartyID); err != nil {
			httpx.NotFoundOr(w, h.d.Log, "configurator party", err, store.ErrNotFound)
			return
		}
		c, ev = nameConfigurator(c, body.ConfiguratorPartyID, actor, now)
	}
	if err := schema.Validate(schema.IDContext, c); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertContext(r.Context(), tx, c); err != nil {
			return err
		}
		if !named {
			return nil
		}
		return appendOwnershipEvent(r.Context(), tx, c.ID, ev)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create project", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.view(c, nil))
}

// listProjects answers one party's stake, never the deployment's whole book.
//
// Exactly one of ownerPartyId and configuratorPartyId is required, and the
// caller is authorized against whichever they gave. Without that narrowing
// this endpoint would tell any signed-in caller which projects exist and who
// configures each — the membership oracle #68 closed on the authorization
// listing, wearing a different query string.
func (h *projectHandlers) listProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	owner, configurator := q.Get("ownerPartyId"), q.Get("configuratorPartyId")
	if (owner == "") == (configurator == "") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query",
			"give exactly one of ownerPartyId or configuratorPartyId; an unnarrowed "+
				"listing answers who configures what to anybody who asks")
		return
	}
	claimed := owner
	if claimed == "" {
		claimed = configurator
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, claimed, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	list, err := listContexts(r.Context(), h.d.DB.Q(), contextQuery{
		OwnerPartyID:        owner,
		ConfiguratorPartyID: configurator,
		Kind:                q.Get("kind"),
		State:               q.Get("state"),
		OwnershipState:      q.Get("ownership"),
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "list projects", err)
		return
	}
	out := make([]projectView, 0, len(list))
	for _, c := range list {
		out = append(out, h.view(c, nil))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": out, "count": len(out)})
}

func (h *projectHandlers) getProject(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	records, err := listContextRecords(r.Context(), h.d.DB.Q(), c.ID, "")
	if err != nil {
		httpx.Fail(w, h.d.Log, "read project records", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(c, records))
}

// nameConfigurator hands a project over, including the re-handover after a
// decline that keeps the Org Admin's queue actionable. It never deletes
// anything: the previous answer stays in the trail.
func (h *projectHandlers) nameConfigurator(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConfiguratorPartyID string `json:"configuratorPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.ConfiguratorPartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "name the party being handed this project")
		return
	}
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	// Naming is the owning organisation's act, never the outgoing
	// configurator's: a configurator who could hand the project on would be
	// delegating an authority that was granted to them personally.
	actor, ok := identity.Authorize(w, r, h.d.Log, c.OwnerPartyID, c.ID,
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), body.ConfiguratorPartyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "configurator party", err, store.ErrNotFound)
		return
	}
	var out schema.Context
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getContextLocked(r.Context(), tx, c.ID, true)
		if err != nil {
			return err
		}
		next, ev := nameConfigurator(fresh, body.ConfiguratorPartyID, actor, h.d.Clock.Now())
		if err := schema.Validate(schema.IDContext, next); err != nil {
			return err
		}
		if err := insertContext(r.Context(), tx, next); err != nil {
			return err
		}
		out = next
		return appendOwnershipEvent(r.Context(), tx, next.ID, ev)
	}); err != nil {
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "name configurator", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(out, nil))
}

// decideOwnership is the receiving side of the handover (n4, design finding
// F2). Accepting and declining are the same shape of act, and declining is not
// an error: it records who declined and why, leaves the project intact, and
// leaves it filterable in the Org Admin's queue as DECLINED.
func (h *projectHandlers) decideOwnership(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	var accept bool
	switch strings.ToLower(strings.TrimSpace(body.Decision)) {
	case "accepted", "accept":
		accept = true
	case "declined", "decline":
		accept = false
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"decision is accepted or declined; there is no third answer and no default")
		return
	}
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if c.Ownership == nil {
		httpx.WriteError(w, http.StatusConflict, "no_handover", "%v", errNoHandover)
		return
	}
	// The named configurator answers, or somebody proven to act for them.
	if _, ok := identity.Authorize(w, r, h.d.Log, c.Ownership.PartyID, c.ID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	var out schema.Context
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getContextLocked(r.Context(), tx, c.ID, true)
		if err != nil {
			return err
		}
		next, ev, err := decideOwnership(fresh, accept, body.Reason,
			fresh.Ownership.PartyID, h.d.Clock.Now())
		if err != nil {
			return err
		}
		if err := schema.Validate(schema.IDContext, next); err != nil {
			return err
		}
		if err := insertContext(r.Context(), tx, next); err != nil {
			return err
		}
		out = next
		return appendOwnershipEvent(r.Context(), tx, next.ID, ev)
	})
	switch {
	case errors.Is(err, errOwnershipAlreadyDecided):
		httpx.WriteError(w, http.StatusConflict, "already_decided", "%v", err)
		return
	case errors.Is(err, errNoHandover):
		httpx.WriteError(w, http.StatusConflict, "no_handover", "%v", err)
		return
	case errors.Is(err, errDeclineNeedsReason):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "%v", err)
		return
	case err != nil:
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "decide ownership", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.view(out, nil))
}

// readOwnership serves the current answer and every answer there has been.
// The trail is the point: after a re-handover the current view says PENDING
// again, and "who declined this, and why" would otherwise be gone.
func (h *projectHandlers) readOwnership(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	events, err := ownershipEvents(r.Context(), h.d.DB.Q(), c.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read ownership trail", err)
		return
	}
	if events == nil {
		events = []ownershipEvent{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"projectId":    c.ID,
		"ownerPartyId": c.OwnerPartyID,
		"ownership":    c.Ownership,
		"events":       events,
	})
}

func (h *projectHandlers) readActivation(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	h.writeActivation(w, c, http.StatusOK)
}

func (h *projectHandlers) writeActivation(w http.ResponseWriter, c schema.Context, status int) {
	conds := activationConditions(c)
	unmet := make([]string, 0, len(conds))
	for _, cond := range conds {
		if !cond.Satisfied {
			unmet = append(unmet, cond.Name)
		}
	}
	httpx.WriteJSON(w, status, map[string]any{
		"projectId":   c.ID,
		"state":       c.State,
		"conditions":  conds,
		"unmet":       unmet,
		"activatable": len(unmet) == 0 && c.State == schema.ContextStateDRAFT,
	})
}

// activateProject is p2_7's button. A refusal answers with every condition and
// which of them are unmet, because "cannot activate" without saying what is
// missing is a dead end, and this system does not leave people at dead ends.
func (h *projectHandlers) activateProject(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.configureProject(w, r, c); !ok {
		return
	}
	var out schema.Context
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getContextLocked(r.Context(), tx, c.ID, true)
		if err != nil {
			return err
		}
		next, _, err := activate(fresh, h.d.Clock.Now())
		if err != nil {
			out = fresh
			return err
		}
		out = next
		return insertContext(r.Context(), tx, next)
	})
	switch {
	case errors.Is(err, errGatesUnmet):
		h.writeActivation(w, out, http.StatusConflict)
		return
	case errors.Is(err, errNotDraft):
		httpx.WriteError(w, http.StatusConflict, "not_draft", "%v", err)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "activate project", err)
		return
	}
	h.writeActivation(w, out, http.StatusOK)
}

func (h *projectHandlers) declareGates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Gates []string `json:"gates"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.configureProject(w, r, c); !ok {
		return
	}
	var out schema.Context
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getContextLocked(r.Context(), tx, c.ID, true)
		if err != nil {
			return err
		}
		next, err := declareGates(fresh, body.Gates, h.d.Clock.Now())
		if err != nil {
			return err
		}
		out = next
		return insertContext(r.Context(), tx, next)
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.NotFoundOr(w, h.d.Log, "project", err, store.ErrNotFound)
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "%v", err)
		return
	}
	h.writeActivation(w, out, http.StatusOK)
}

func (h *projectHandlers) satisfyGate(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.configureProject(w, r, c); !ok {
		return
	}
	var out schema.Context
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getContextLocked(r.Context(), tx, c.ID, true)
		if err != nil {
			return err
		}
		next, err := satisfyGate(fresh, r.PathValue("name"), h.d.Clock.Now())
		if err != nil {
			return err
		}
		out = next
		return insertContext(r.Context(), tx, next)
	})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.NotFoundOr(w, h.d.Log, "project", err, store.ErrNotFound)
			return
		}
		httpx.WriteError(w, http.StatusNotFound, "no_such_gate", "%v", err)
		return
	}
	h.writeActivation(w, out, http.StatusOK)
}

// roleGrant is one grant as the People & roles screens read it: who holds
// what, since when, from whom, and whether it still stands. Every field is
// already on the Authorization primitive — "a role is held, not just recorded"
// needed no new storage, only a read that names the grantor.
type roleGrant struct {
	GrantID          string              `json:"grantId"`
	PartyID          string              `json:"partyId"`
	DisplayName      string              `json:"displayName,omitempty"`
	PartyKind        string              `json:"partyKind,omitempty"`
	Functions        []string            `json:"functions"`
	GrantedByPartyID string              `json:"grantedByPartyId"`
	AuthorityPartyID string              `json:"authorityPartyId"`
	GrantedAt        time.Time           `json:"grantedAt"`
	From             time.Time           `json:"from"`
	Until            *time.Time          `json:"until,omitempty"`
	State            string              `json:"state"`
	RevokedAt        *time.Time          `json:"revokedAt,omitempty"`
	Terms            schema.VersionedRef `json:"terms"`
	ReviewBy         *time.Time          `json:"reviewBy,omitempty"`
}

func (h *projectHandlers) roleGrants(r *http.Request, grants []schema.Authorization) []roleGrant {
	out := make([]roleGrant, 0, len(grants))
	for _, a := range grants {
		g := roleGrant{
			GrantID:          a.ID,
			PartyID:          a.PartyID,
			Functions:        a.Functions,
			GrantedByPartyID: a.ApprovedByPartyID,
			AuthorityPartyID: a.AuthorityPartyID,
			GrantedAt:        a.ApprovedAt,
			From:             a.Period.Start,
			Until:            a.Period.End,
			State:            string(a.State),
			RevokedAt:        a.RevokedAt,
			Terms:            a.Terms,
			ReviewBy:         a.ReviewBy,
		}
		// The display name is best-effort: a screen that names the grantor and
		// leaves the holder as an opaque id is a screen nobody can read, and a
		// party that has since been merged away must not fail the whole list.
		if p, err := getParty(r.Context(), h.d.DB.Q(), a.PartyID); err == nil {
			g.DisplayName, g.PartyKind = p.DisplayName, string(p.Kind)
		}
		out = append(out, g)
	}
	return out
}

// listProjectRoles is n5's read-only half and p2_6's list: who holds a role on
// this project, with the grant date and the grantor named from real
// authorization data.
//
// Not the roster query #68 refused. That refusal protects "where does this
// person work" asked about an arbitrary person; this answers "who holds a role
// on this project" to the project's own organisation. Worker participation is
// a Claim, not an Authorization, so no worker appears here by holding work.
func (h *projectHandlers) listProjectRoles(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	grants, err := contextGrants(r.Context(), h.d.DB.Q(), c.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list project roles", err)
		return
	}
	roles := h.roleGrants(r, grants)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"projectId": c.ID, "roles": roles, "count": len(roles),
		// n5 asks a guard to name somebody who can grant the role. The owning
		// organisation is that answer, and it is a fact of the record rather
		// than a string a screen hard-codes.
		"grantableBy": c.OwnerPartyID,
	})
}

// listOrganisationRoles is p1_2: the standing configuration's people, at
// instance scope, under this organisation's authority.
func (h *projectHandlers) listOrganisationRoles(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if _, ok := identity.Authorize(w, r, h.d.Log, orgID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	grants, err := authorityGrants(r.Context(), h.d.DB.Q(), orgID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list organisation roles", err)
		return
	}
	roles := h.roleGrants(r, grants)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"organisationId": orgID, "roles": roles, "count": len(roles), "grantableBy": orgID,
	})
}

// grantRequest is a role or partner grant as the console poses it. `functions`
// is the whole role vocabulary — an L2 list of opaque strings — and the core
// stores it without knowing what any of the words mean.
type grantRequest struct {
	PartyID   string               `json:"partyId"`
	Functions []string             `json:"functions"`
	Terms     *schema.VersionedRef `json:"terms"`
	Period    *schema.Period       `json:"period"`
	ReviewBy  *time.Time           `json:"reviewBy"`
	Evidence  []struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"evidence"`
}

// grantProjectRole grants a context-scoped role on this project (p2_6, and the
// role a configurator needs before n5 stops guarding them).
//
// The grant is an Authorization, unchanged: the authority is the project's
// owning organisation, the scope is this project, the grantor is whoever
// proved they may act — and grantor and grant date were already fields on the
// primitive, which is why "a role is held, not just recorded" needed no
// storage change.
func (h *projectHandlers) grantProjectRole(w http.ResponseWriter, r *http.Request) {
	var body grantRequest
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	// Granting a role is the owning organisation's act. Deliberately not
	// configureProject: a configurator reading n4 must not be able to grant
	// themselves anything.
	actor, ok := identity.Authorize(w, r, h.d.Log, c.OwnerPartyID, c.ID,
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	if _, ok := h.requireApprovedOrganisation(w, r, c.OwnerPartyID, "the project's owning organisation"); !ok {
		return
	}
	h.writeGrant(w, r, c, body, actor, false)
}

// grantPartner is p2_18: a grant narrower than the terms, that ends.
//
// Three rules, all of them infrastructure. The partner must be an APPROVED
// organisation, because the directory's promise is that onboarding already
// happened. The grant must have an end date, because "the grant lapses on 31
// March by itself" is the screen's whole subject and a grant with no end is a
// different thing. And the functions must be covered by the terms the partner
// actually accepted, because a grant wider than the terms is a permission
// nobody agreed to.
func (h *projectHandlers) grantPartner(w http.ResponseWriter, r *http.Request) {
	var body grantRequest
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	actor, ok := h.configureProject(w, r, c)
	if !ok {
		return
	}
	if body.Period == nil || body.Period.End == nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "grant_must_end",
			"a partner grant carries an end date; permission to do more must stop by itself, "+
				"and what was already validated stays validated when it does")
		return
	}
	if _, ok := h.requireApprovedOrganisation(w, r, body.PartyID, "partyId"); !ok {
		return
	}
	terms, err := acceptedTerms(r.Context(), h.d.DB.Q(), body.PartyID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(w, http.StatusConflict, "no_accepted_terms",
				"this organisation has accepted no terms, so there is nothing for a grant to be narrower than")
			return
		}
		httpx.Fail(w, h.d.Log, "read accepted terms", err)
		return
	}
	if missing := narrowerThanTerms(body.Functions, terms.Permissions); len(missing) > 0 {
		httpx.WriteProblems(w, "wider_than_terms",
			"a grant cannot exceed the terms the organisation accepted", missing)
		return
	}
	// The grant rides the partner's own accepted terms version, not a version
	// the caller chose: which terms an organisation held is the fact a verifier
	// walks back to, and letting a project name a different one would make the
	// grant unwalkable.
	body.Terms = &schema.VersionedRef{ID: terms.ID, Version: terms.Version}
	h.writeGrant(w, r, c, body, actor, true)
}

func (h *projectHandlers) writeGrant(w http.ResponseWriter, r *http.Request, c schema.Context,
	body grantRequest, actor string, partner bool) {

	if body.PartyID == "" || len(body.Functions) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a grant needs a party and at least one function; a role with no functions is a label")
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), body.PartyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "party", err, store.ErrNotFound)
		return
	}
	now := h.d.Clock.Now()
	terms := body.Terms
	if terms == nil {
		// A role grant under an organisation's authority stands on the terms
		// that organisation itself accepted. Defaulted rather than required
		// because the console does not know them; refused rather than
		// invented when there are none.
		t, err := acceptedTerms(r.Context(), h.d.DB.Q(), c.OwnerPartyID)
		if err != nil {
			httpx.WriteError(w, http.StatusConflict, "no_terms_for_grant",
				"name the terms this grant stands on: the owning organisation has "+
					"accepted none, and CREST will not invent a permission set")
			return
		}
		terms = &schema.VersionedRef{ID: t.ID, Version: t.Version}
	}
	period := schema.Period{Start: now}
	if body.Period != nil {
		period = *body.Period
		if period.Start.IsZero() {
			period.Start = now
		}
	}
	ctxID := c.ID
	a := schema.Authorization{
		ID:      id.New(h.d.Clock, "authorization"),
		PartyID: body.PartyID,
		Terms:   *terms,
		Scope: schema.AuthorizationScope{
			Kind: schema.AuthorizationScopeKindContext, ContextID: &ctxID,
		},
		Functions:         body.Functions,
		Period:            period,
		ReviewBy:          body.ReviewBy,
		AuthorityPartyID:  c.OwnerPartyID,
		ApprovedByPartyID: cmpOr(actor, c.OwnerPartyID),
		ApprovedAt:        now,
		State:             schema.AuthorizationStateACTIVE,
	}
	for _, e := range body.Evidence {
		a.Evidence = append(a.Evidence, schema.AuthorizationEvidenceItem{Kind: e.Kind, Ref: e.Ref})
	}
	if err := schema.Validate(schema.IDAuthorization, a); err != nil {
		writeValidation(w, err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertAuthorization(r.Context(), tx, a); err != nil {
			return err
		}
		// Same enqueue as POST /v1/authorizations: the delivery path decides
		// what may be published, and refuses with a reason rather than
		// dropping a fact silently here.
		return enqueueFact(r.Context(), tx, "authorization", a.ID, 1)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "grant role", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.roleGrants(r, []schema.Authorization{a})[0])
}

// listPartnerGrants is the same context grants, narrowed to organisations —
// what p2_18 produced, read back.
func (h *projectHandlers) listPartnerGrants(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	grants, err := contextGrants(r.Context(), h.d.DB.Q(), c.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list partner grants", err)
		return
	}
	all := h.roleGrants(r, grants)
	out := make([]roleGrant, 0, len(all))
	for _, g := range all {
		if g.PartyKind == string(schema.PartyKindOrganisation) {
			out = append(out, g)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"projectId": c.ID, "grants": out, "count": len(out),
	})
}

// partnerDirectory is p2_17: the approved organisations, which is what
// registration was for.
//
// Nothing here re-examines an organisation and nothing here can change one —
// the read is a join over decisions somebody else already made, some of them
// years ago. The public organisation face is untouched: this is an
// authenticated read of registry facts, and self-declared attributes appear
// here precisely because they must never appear on the published face.
func (h *projectHandlers) partnerDirectory(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	entries, err := approvedOrganisations(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "read partner directory", err)
		return
	}
	q := r.URL.Query()
	f := directoryFilter{
		Sector:      q.Get("sector"),
		Country:     q.Get("country"),
		Permissions: q["permission"],
	}
	out := make([]directoryEntry, 0, len(entries))
	for _, e := range entries {
		if matchesDirectory(e, f) {
			out = append(out, e)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"partners": out, "count": len(out)})
}

// putComposition records one composition choice with its decider (p2_1, p2_3,
// p2_5). The choice's name and its value are both the deployment's vocabulary;
// what infrastructure contributes is that somebody's name and a timestamp are
// attached to every answer, and that answering one choice never overwrites
// another.
func (h *projectHandlers) putComposition(w http.ResponseWriter, r *http.Request) {
	choice := r.PathValue("choice")
	if err := validRecordKind(choice); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_choice", "%v", err)
		return
	}
	var body struct {
		Value any    `json:"value"`
		Note  string `json:"note"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.Value == nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a composition choice needs a value; an unanswered choice is the absence of a record, not a null")
		return
	}
	h.record(w, r, recordCompositionPfx+choice, map[string]any{
		"choice": choice, "value": body.Value, "note": body.Note,
	})
}

func (h *projectHandlers) readComposition(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	records, err := listContextRecords(r.Context(), h.d.DB.Q(), c.ID, recordCompositionPfx)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read composition", err)
		return
	}
	if records == nil {
		records = []contextRecord{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"projectId": c.ID, "choices": records, "count": len(records),
	})
}

// record is the one write path for every configuration-level fact keyed to a
// project. It exists once because the accountability is the same every time:
// the acting party and the service's clock, never the caller's word for
// either.
func (h *projectHandlers) record(w http.ResponseWriter, r *http.Request, kind string, payload any) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	actor, ok := h.configureProject(w, r, c)
	if !ok {
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return putContextRecord(r.Context(), tx, c.ID, kind,
			payload, cmpOr(actor, c.OwnerPartyID), h.d.Clock.Now())
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record "+kind, err)
		return
	}
	rec, err := getContextRecord(r.Context(), h.d.DB.Q(), c.ID, kind)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read back "+kind, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *projectHandlers) readRecord(w http.ResponseWriter, r *http.Request, kind string) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	rec, err := getContextRecord(r.Context(), h.d.DB.Q(), c.ID, kind)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, kind, err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

// putFinanceLink is p2_8. CREST does not invent account codes: the code
// arrives from a finance system that already had it, is stored verbatim, and
// is never generated, formatted or reserved here.
func (h *projectHandlers) putFinanceLink(w http.ResponseWriter, r *http.Request) {
	var l financeLink
	if !httpx.ReadJSON(w, r, &l) {
		return
	}
	if err := validFinanceLink(l); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_finance_link", "%v", err)
		return
	}
	h.record(w, r, recordFinanceLink, l)
}

func (h *projectHandlers) readFinanceLink(w http.ResponseWriter, r *http.Request) {
	h.readRecord(w, r, recordFinanceLink)
}

// putSupportOwner is p2_10: support belongs to the project, not the platform.
// The owner must resolve to a Party, because a support owner nobody can name
// is the dead end that leaves a worker with a missing payment and no
// explanation attached.
func (h *projectHandlers) putSupportOwner(w http.ResponseWriter, r *http.Request) {
	var s supportOwner
	if !httpx.ReadJSON(w, r, &s) {
		return
	}
	if err := validSupportOwner(s); err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_support_owner", "%v", err)
		return
	}
	if _, err := getParty(r.Context(), h.d.DB.Q(), s.PartyID); err != nil {
		httpx.NotFoundOr(w, h.d.Log, "support owner party", err, store.ErrNotFound)
		return
	}
	if s.EscalatesToPartyID != "" {
		if _, err := getParty(r.Context(), h.d.DB.Q(), s.EscalatesToPartyID); err != nil {
			httpx.NotFoundOr(w, h.d.Log, "escalation party", err, store.ErrNotFound)
			return
		}
	}
	h.record(w, r, recordSupportOwner, s)
}

func (h *projectHandlers) readSupportOwner(w http.ResponseWriter, r *http.Request) {
	h.readRecord(w, r, recordSupportOwner)
}

// cmpOr is the first non-empty of two strings. Small enough to inline, named
// because "the actor, or the party they acted for" reads as a decision rather
// than as an expression.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
