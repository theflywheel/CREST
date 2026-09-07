package parties

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The G-2 onboarding-journey surface: invitations (g2_5, g2_9, g2_10) and
// terms-upgrade requests with their pre-live checks (g2_6–g2_8, g2_11, g2_12).
// Blueprint §15 J1 names both objects at L1.
// A terms upgrade always requires a named human decision, whatever
// REGISTRY_ORG_APPROVAL says about first registration: automatic approval on
// acceptance exists so a pilot can onboard known partners, but wider terms are
// the moment an organisation gains powers it did not have, and that moment
// keeps a person's name on it.
func registerOnboardingJourneyRoutes(mux *http.ServeMux, d service.Deps) {
	h := &g2Handlers{projectHandlers: projectHandlers{d: d}}

	// The offer, both sides of it.
	mux.HandleFunc("POST /v1/projects/{id}/invitations", h.sendInvitation)
	mux.HandleFunc("GET /v1/projects/{id}/invitations", h.listProjectInvitations)
	mux.HandleFunc("GET /v1/organisations/{id}/invitations", h.listOrganisationInvitations)
	mux.HandleFunc("GET /v1/invitations/{id}", h.getInvitation)
	mux.HandleFunc("POST /v1/invitations/{id}/decision", h.decideInvitation)
	mux.HandleFunc("POST /v1/invitations/{id}/questions", h.askInvitationQuestion)

	// The wider-terms request and what is checked before it is live.
	mux.HandleFunc("POST /v1/organisations/{id}/terms-requests", h.createTermsRequest)
	mux.HandleFunc("GET /v1/organisations/{id}/terms-requests", h.listOrganisationTermsRequests)
	mux.HandleFunc("GET /v1/terms-requests", h.listTermsRequestQueue)
	mux.HandleFunc("GET /v1/terms-requests/{id}", h.getTermsRequest)
	mux.HandleFunc("PUT /v1/terms-requests/{id}/documents", h.replaceRequestDocuments)
	mux.HandleFunc("POST /v1/terms-requests/{id}/submit", h.submitTermsRequest)
	mux.HandleFunc("POST /v1/terms-requests/{id}/withdraw", h.withdrawTermsRequest)
	mux.HandleFunc("POST /v1/terms-requests/{id}/checks", h.recordCheck)
	mux.HandleFunc("GET /v1/terms-requests/{id}/checks", h.listChecks)
	mux.HandleFunc("POST /v1/terms-requests/{id}/decision", h.decideTermsRequest)
}

type g2Handlers struct {
	projectHandlers
}

// sendInvitation is the project's offer. The inviter must hold configuration
// authority on the project (the same gate as a partner grant, because the
// acceptance will create one); the invitee must be an organisation Party, but
// its registration need not be decided yet — g2_5's point is that registration
// stands alone, so an offer may arrive before or after the registry's own
// decision. Acceptance, not sending, is where approval is checked.
func (h *g2Handlers) sendInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PartyID   string         `json:"partyId"`
		Functions []string       `json:"functions"`
		Period    *schema.Period `json:"period"`
		Note      string         `json:"note"`
	}
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
	invitee, err := getParty(r.Context(), h.d.DB.Q(), body.PartyID)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "invited party", err, store.ErrNotFound)
		return
	}
	if invitee.Kind != schema.PartyKindOrganisation {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "not_an_organisation",
			"a project invites an organisation; a person joins through a role grant, not an invitation")
		return
	}
	inv, ev, err := newInvitation(id.New(h.d.Clock, "invitation"), c.ID, body.PartyID,
		body.Functions, body.Period, body.Note, cmpOr(actor, c.OwnerPartyID), h.d.Clock.Now())
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_invitation", "%v", err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := upsertInvitation(r.Context(), tx, inv); err != nil {
			return err
		}
		return appendInvitationEvent(r.Context(), tx, inv.ID, ev)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "send invitation", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, inv)
}

func (h *g2Handlers) listProjectInvitations(w http.ResponseWriter, r *http.Request) {
	c, ok := h.project(w, r)
	if !ok {
		return
	}
	if _, ok := h.viewProject(w, r, c); !ok {
		return
	}
	list, err := listInvitations(r.Context(), h.d.DB.Q(), c.ID, "", r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list project invitations", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invitations": list, "count": len(list)})
}

// listOrganisationInvitations is the organisation's inbox (g2_9's source).
// Narrowed to the organisation and authorized against it, for the same reason
// GET /v1/projects narrows: who is courting whom is not a public listing.
func (h *g2Handlers) listOrganisationInvitations(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if _, ok := identity.Authorize(w, r, h.d.Log, orgID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	list, err := listInvitations(r.Context(), h.d.DB.Q(), "", orgID, r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list organisation invitations", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invitations": list, "count": len(list)})
}

// getInvitation serves one offer with its whole trail — the questions
// included, because a question asked on the record is on the record.
func (h *g2Handlers) getInvitation(w http.ResponseWriter, r *http.Request) {
	inv, err := getInvitation(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "invitation", err, store.ErrNotFound)
		return
	}
	// Two ways in: the invited organisation itself, or anybody the project's
	// own view gate admits. The invitee check is on the proven caller directly
	// — the same shape as isNamedConfigurator — so an invited organisation can
	// read its own offer without holding anything on the project yet.
	caller := identity.From(r.Context())
	if caller.PartyID != inv.PartyID {
		c, err := getContext(r.Context(), h.d.DB.Q(), inv.ContextID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "read invitation project", err)
			return
		}
		if _, ok := h.viewProject(w, r, c); !ok {
			return
		}
	}
	events, err := invitationEvents(r.Context(), h.d.DB.Q(), inv.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read invitation trail", err)
		return
	}
	if events == nil {
		events = []invitationEvent{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invitation": inv, "events": events})
}

// decideInvitation is g2_9's Accept and Decline. Declining records who and why
// and destroys nothing. Accepting is where the ordering gate lives: the
// organisation must by now be APPROVED and hold accepted terms, and the offer's
// functions must fit inside them — because acceptance creates the partner
// grant, the very grant p2_18 writes, riding the organisation's own accepted
// terms version.
func (h *g2Handlers) decideInvitation(w http.ResponseWriter, r *http.Request) {
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
	inv, err := getInvitation(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "invitation", err, store.ErrNotFound)
		return
	}
	// The invited organisation answers, or somebody proven to act for it.
	actor, ok := identity.Authorize(w, r, h.d.Log, inv.PartyID, inv.ContextID,
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	var terms schema.Terms
	if accept {
		// Registration before OR after the invitation — but always before the
		// acceptance, because this is the moment a standing grant is written.
		// A missing registration and an undecided one get the same patient
		// answer: mayAcceptInvitation says not yet, never no.
		reg, err := getRegistration(r.Context(), h.d.DB.Q(), inv.PartyID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			httpx.Fail(w, h.d.Log, "read invitee registration", err)
			return
		}
		if err := mayAcceptInvitation(reg.State); err != nil {
			httpx.WriteError(w, http.StatusConflict, "organisation_not_approved", "%v", err)
			return
		}
		terms, err = acceptedTerms(r.Context(), h.d.DB.Q(), inv.PartyID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				httpx.WriteError(w, http.StatusConflict, "no_accepted_terms",
					"this organisation has accepted no terms, so there is nothing for the grant to be narrower than")
				return
			}
			httpx.Fail(w, h.d.Log, "read accepted terms", err)
			return
		}
		if missing := narrowerThanTerms(inv.Functions, terms.Permissions); len(missing) > 0 {
			// The offer outgrew the terms — possible when the org accepted a
			// narrower set after the offer was sent. Refused with every
			// uncovered function named, not just the first.
			httpx.WriteProblems(w, "wider_than_terms",
				"this invitation offers more than the terms the organisation accepted; it cannot be accepted as it stands", missing)
			return
		}
	}
	now := h.d.Clock.Now()
	var out invitation
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		fresh, err := getInvitation(r.Context(), tx, inv.ID, true)
		if err != nil {
			return err
		}
		next, ev, err := decideInvitation(fresh, accept, body.Reason, cmpOr(actor, inv.PartyID), now)
		if err != nil {
			return err
		}
		if accept {
			// The acceptance IS the grant: one transaction, so there is no
			// state in which the org said yes and holds nothing (g2_10).
			ctxID := next.ContextID
			a := schema.Authorization{
				ID:      id.New(h.d.Clock, "authorization"),
				PartyID: next.PartyID,
				Terms:   schema.VersionedRef{ID: terms.ID, Version: terms.Version},
				Scope: schema.AuthorizationScope{
					Kind: schema.AuthorizationScopeKindContext, ContextID: &ctxID,
				},
				Functions:         next.Functions,
				Period:            next.Period,
				ApprovedByPartyID: identity.From(r.Context()).PartyID,
				ApprovedAt:        now,
				State:             schema.AuthorizationStateACTIVE,
			}
			// The authority behind the grant is the project's owning
			// organisation, not the individual who clicked send.
			c, err := getContext(r.Context(), tx, next.ContextID)
			if err != nil {
				return err
			}
			a.AuthorityPartyID = c.OwnerPartyID
			ownerTerms, termsErr := acceptedTerms(r.Context(), tx, c.OwnerPartyID)
			if termsErr != nil {
				return termsErr
			}
			if missing := narrowerThanTerms(a.Functions, ownerTerms.Permissions); len(missing) > 0 {
				return fmt.Errorf("invitation exceeds authority terms: %s", strings.Join(missing, ", "))
			}
			if err := schema.Validate(schema.IDAuthorization, a); err != nil {
				return err
			}
			if err := insertAuthorization(r.Context(), tx, a); err != nil {
				return err
			}
			if err := enqueueFact(r.Context(), tx, "authorization", a.ID, 1); err != nil {
				return err
			}
			next.GrantID = &a.ID
		}
		if err := upsertInvitation(r.Context(), tx, next); err != nil {
			return err
		}
		out = next
		return appendInvitationEvent(r.Context(), tx, next.ID, ev)
	})
	switch {
	case errors.Is(err, errInvitationDecided):
		httpx.WriteError(w, http.StatusConflict, "already_decided", "%v", err)
		return
	case errors.Is(err, errInvitationDeclineNeedsReason):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "reason_required", "%v", err)
		return
	case err != nil:
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "decide invitation", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// askInvitationQuestion is g2_9's third button. Either side may ask while the
// offer is open; the asker is named in the body and proven, because a question
// on the record needs a person on the record.
func (h *g2Handlers) askInvitationQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AskedBy string `json:"askedBy"`
		Text    string `json:"text"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.AskedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"askedBy names who is asking; an anonymous question has nowhere to send the answer")
		return
	}
	inv, err := getInvitation(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "invitation", err, store.ErrNotFound)
		return
	}
	actor, ok := identity.Authorize(w, r, h.d.Log, body.AskedBy, inv.ContextID,
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	// The conversation belongs to its two sides: the invited organisation, and
	// the project's own people (owner or named configurator).
	if body.AskedBy != inv.PartyID {
		c, err := getContext(r.Context(), h.d.DB.Q(), inv.ContextID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "read invitation project", err)
			return
		}
		if body.AskedBy != c.OwnerPartyID && !isNamedConfigurator(c, body.AskedBy) {
			httpx.WriteError(w, http.StatusForbidden, "not_a_party_to_this",
				"questions on an invitation belong to the invited organisation and the inviting project")
			return
		}
	}
	ev, err := askQuestion(inv, cmpOr(actor, body.AskedBy), body.Text, h.d.Clock.Now())
	if err != nil {
		if errors.Is(err, errQuestionAfterAnswer) {
			httpx.WriteError(w, http.StatusConflict, "already_decided", "%v", err)
			return
		}
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid_question", "%v", err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return appendInvitationEvent(r.Context(), tx, inv.ID, ev)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record question", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ev)
}
