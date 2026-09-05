package parties

import (
	"errors"
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// registerOrganisation is an organisation applying for itself (#20).
//
// It creates the Party and the application in one transaction. Two calls would
// leave a window where an organisation exists with nobody having applied for
// it, and a Party with no application is a Party no approval path can ever
// reach — it would simply sit there looking legitimate.
func (h *handlers) registerOrganisation(w http.ResponseWriter, r *http.Request) {
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
	if p.Kind == "" {
		p.Kind = schema.PartyKindOrganisation
	}
	if p.Kind != schema.PartyKindOrganisation {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "not_an_organisation",
			"this endpoint registers organisations; a person is enrolled, not registered")
		return
	}
	if err := schema.Validate(schema.IDParty, p); err != nil {
		writeValidation(w, err)
		return
	}

	var (
		reg        Registration
		inviteCode string
	)
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertParty(r.Context(), tx, p); err != nil {
			return err
		}
		if err := insertRegistration(r.Context(), tx, p.ID, h.d.Clock.Now()); err != nil {
			return err
		}
		var err error
		reg, err = getRegistration(r.Context(), tx, p.ID)
		if err != nil {
			return err
		}
		// The applicant registered at an open door and holds no session as
		// the organisation yet. The code is how they come back as it: their
		// own login claims the unbound organisation party (g2_13's "copy the
		// key"). Invited by the party itself — nobody with standing exists
		// yet, and that is the honest record of an open-door registration.
		inviteCode, err = mintInvitation(r.Context(), tx, p.ID, p.ID, h.d.Clock.Now(), 0)
		return err
	})
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		httpx.WriteError(w, http.StatusConflict, "already_applied", "%s", ErrAlreadyApplied)
	case err != nil:
		httpx.Fail(w, h.d.Log, "register organisation", err)
	default:
		// The organisation is NOT published here. An applicant is not an
		// approved organisation, and an append-only log that recorded every
		// application as an existing organisation could not later distinguish
		// the two (§3).
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"party": p, "registration": reg, "inviteCode": inviteCode,
		})
	}
}

// listRegistrations is the reviewer's queue read (g4_1): every registration a
// person may still have to look at, undecided first. Authenticated like the
// terms-request queue — who reviews is L2 routing this service does not
// encode, and nothing here is identity data: the row carries the applicant's
// display name and self-declared attributes, the same facts the
// applicant-facing registration read already serves (#168).
func (h *handlers) listRegistrations(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	state := r.URL.Query().Get("state")
	switch state {
	case "", stateApplied, stateTermsAccepted, stateApproved, stateRejected:
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query",
			"state must be APPLIED, TERMS_ACCEPTED, APPROVED or REJECTED, or absent for all")
		return
	}
	list, err := listRegistrations(r.Context(), h.d.DB.Q(), state)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list registrations", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"registrations": list, "count": len(list)})
}

func (h *handlers) getRegistration(w http.ResponseWriter, r *http.Request) {
	reg, err := getRegistration(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "registration", err, store.ErrNotFound)
		return
	}
	// The organisation's self-declared attributes (kind, sector, country,
	// contact person — #168) ride the registration read: this is the surface
	// the applicant's own status screen polls, and serving the profile here
	// proves the round-trip without opening the Party document itself, which
	// stays behind the authorized read (#102). Attributes are descriptive
	// facts the organisation asserted about itself — never identity data, the
	// schema forbids more than bounded strings — so the applicant-facing
	// registration read may carry them.
	party, err := getParty(r.Context(), h.d.DB.Q(), reg.PartyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read registration party", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Registration
		// The display name rides for the same reason the attributes do: it is
		// the organisation's own self-declared public name, and both the
		// applicant's status screen and the reviewer's detail need a name to
		// put on the record they are reading.
		DisplayName string         `json:"displayName,omitempty"`
		Attributes  map[string]any `json:"attributes,omitempty"`
	}{reg, party.DisplayName, party.Attributes})
}

// acceptTerms records agreement to a specific terms version.
func (h *handlers) acceptTerms(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TermsID      string `json:"termsId"`
		TermsVersion int    `json:"termsVersion"`
		AcceptedBy   string `json:"acceptedBy"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	// The version is required rather than defaulted to the latest. "Whatever
	// was current at the time" is not a fact a verifier can walk back to, and
	// this is precisely the fact they walk back to from a credential.
	if body.TermsID == "" || body.TermsVersion == 0 || body.AcceptedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"termsId, termsVersion and acceptedBy are all required; an acceptance with no version or no acceptor cannot be checked later")
		return
	}
	partyID := r.PathValue("id")

	var reg Registration
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if _, err := getTerms(r.Context(), tx, body.TermsID, body.TermsVersion); err != nil {
			// Accepting terms that do not exist is an acceptance of nothing.
			return err
		}
		var err error
		reg, err = acceptTerms(r.Context(), tx, partyID, body.TermsID, body.TermsVersion,
			body.AcceptedBy, h.d.Clock.Now())
		if err != nil {
			return err
		}
		// Automatic approval, where the deployment chose it, happens here — in
		// the same transaction as the acceptance, so there is no state in which
		// terms are accepted and the approval that was meant to follow did not.
		if h.model == approvalOnTerms {
			reg, err = decide(r.Context(), tx, partyID, true,
				approvalByPolicy, "approved automatically on terms acceptance (REGISTRY_ORG_APPROVAL=on-terms-acceptance)",
				h.d.Clock.Now(), h.model)
			if err != nil {
				return err
			}
			return enqueueFact(r.Context(), tx, "organisation", partyID, 1)
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrAlreadyDecided):
		httpx.WriteError(w, http.StatusConflict, "already_decided", "%s", ErrAlreadyDecided)
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no such organisation application, or no such terms version")
	case err != nil:
		httpx.Fail(w, h.d.Log, "accept terms", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, reg)
	}
}

// approvalByPolicy is the decider recorded when the deployment approves
// automatically. It is a named value rather than an empty string because "every
// held payment has a reason with an owner" generalises: a decision with no
// owner recorded is a decision nobody can be asked about, and "the policy did
// it" is at least an answer someone can trace to a configuration change.
const approvalByPolicy = "crest:policy:on-terms-acceptance"

func (h *handlers) decideRegistration(w http.ResponseWriter, r *http.Request) {
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
	// decidedBy is now checked rather than recorded (#89).
	//
	// The self-approval constraint below — decide() refuses when decidedBy is
	// the applicant — was always a check on a value the caller chose, which
	// made it a rule an applicant could satisfy by naming somebody else. It is
	// a real constraint only once the name has been proved, and that is what
	// this line adds.
	decidedBy, ok := identity.Authorize(w, r, h.d.Log, body.DecidedBy, "",
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	body.DecidedBy = decidedBy
	if !body.Approve && body.Reason == "" {
		// The same rule as a held payment: a refusal with no reason attached
		// leaves the applicant with a closed door and no explanation.
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a rejection needs a reason")
		return
	}
	partyID := r.PathValue("id")

	var reg Registration
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		reg, err = decide(r.Context(), tx, partyID, body.Approve, body.DecidedBy, body.Reason,
			h.d.Clock.Now(), h.model)
		if err != nil {
			return err
		}
		if !body.Approve {
			return nil
		}
		// An approved organisation is a public fact, and only now. Publication
		// is enqueued in the transaction that approved it (§3).
		return enqueueFact(r.Context(), tx, "organisation", partyID, 1)
	})
	switch {
	case errors.Is(err, ErrSelfApproved):
		httpx.WriteError(w, http.StatusConflict, "self_approved", "%s", ErrSelfApproved)
	case errors.Is(err, ErrTermsNotAccepted):
		httpx.WriteError(w, http.StatusConflict, "terms_not_accepted", "%s", ErrTermsNotAccepted)
	case errors.Is(err, ErrAlreadyDecided):
		httpx.WriteError(w, http.StatusConflict, "already_decided", "%s", ErrAlreadyDecided)
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such organisation application")
	case err != nil:
		httpx.Fail(w, h.d.Log, "decide registration", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, reg)
	}
}

// assistedEnrolment registers a worker who is not holding a phone (#20).
//
// W1: a worker must be able to exist without a document, a phone or literacy. A
// system that can only enrol people who can complete a form on their own device
// excludes exactly the workers it is for. So a field worker enrols them, and
// who did it is written down.
//
// What this deliberately does NOT do is raise the worker's identity assurance.
// The enroller's attestation is provenance, not proof: assurance stays derived
// from the Party's own identityBindings, so a worker enrolled this way and one
// who bound an anchor themselves are not silently treated as equivalent, and
// the first can be upgraded later without anything being rewritten.
func (h *handlers) assistedEnrolment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Party      schema.Party `json:"party"`
		EnrolledBy string       `json:"enrolledBy"`
		ContextID  *string      `json:"contextId,omitempty"`
		Method     string       `json:"method"`
		Note       *string      `json:"note,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.EnrolledBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"enrolledBy is required; an assisted enrolment with no enroller is an unattributed one")
		return
	}
	// The enroller is the caller (#102): this is the checked door into the
	// registry for somebody who cannot register themselves, and the name on
	// the assistance has to be a name that was proven.
	ctxID := ""
	if body.ContextID != nil {
		ctxID = *body.ContextID
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, body.EnrolledBy, ctxID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	if !validEnrolmentMethod(body.Method) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"method must be supervisor-attested, roster-import, field-visit or confidence-check")
		return
	}

	p := body.Party
	if p.ID == "" {
		p.ID = id.Party(h.d.Clock)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = h.d.Clock.Now()
	}
	if p.Kind == "" {
		p.Kind = schema.PartyKindPerson
	}
	if p.ID == body.EnrolledBy {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "self_enrolled",
			"an assisted enrolment is by someone else; this is ordinary registration")
		return
	}
	// A worker with no phone still needs a route someone can reach them by,
	// which is what the 'supervisor' contact kind is for. The schema requires
	// at least one route and this path does not exempt it: W2 is unenforceable
	// against a Party nobody can reach, and an unreachable worker is one who
	// cannot be told their payment is held.
	if err := schema.Validate(schema.IDParty, p); err != nil {
		writeValidation(w, err)
		return
	}

	enrolment := Enrolment{
		PartyID:    p.ID,
		EnrolledBy: body.EnrolledBy,
		ContextID:  body.ContextID,
		Method:     body.Method,
		Note:       body.Note,
		EnrolledAt: h.d.Clock.Now(),
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertParty(r.Context(), tx, p); err != nil {
			return err
		}
		return insertEnrolment(r.Context(), tx, enrolment)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "assisted enrolment", err)
		return
	}
	// No publication. A worker is personal data and never reaches the node —
	// this is the "never the reverse" half of §3's placement rule.
	level, because := assuranceOf(p, h.d.Clock.Now())
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"party":     p,
		"enrolment": enrolment,
		// Returned so the caller can see immediately that being enrolled by a
		// supervisor did not, by itself, raise the worker's assurance.
		"identityAssurance": level,
		"because":           because,
	})
}

// getEnrolment answers "who enrolled this worker, and how?".
//
// Exposed because it is provenance somebody may later be asked about — a
// supervisor who attested to a worker's existence is answerable for that — and
// provenance nobody can read is provenance nobody can be held to.
func (h *handlers) getEnrolment(w http.ResponseWriter, r *http.Request) {
	// Who enrolled this worker is the worker's record (#102). contextId scopes
	// the actor check, as on bindings.
	if _, ok := identity.Authorize(w, r, h.d.Log, r.PathValue("id"),
		r.URL.Query().Get("contextId"), h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	e, err := getEnrolment(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		// Distinguished from "no such party": most workers are not assisted
		// enrolments, and answering 404 for both would make "was this worker
		// enrolled by someone else?" unanswerable.
		httpx.WriteError(w, http.StatusNotFound, "not_assisted",
			"this party was not enrolled by someone else")
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "read enrolment", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, e)
}

// publication answers where a public fact landed, and whether anyone outside
// CREST can check it.
func (h *handlers) publication(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if _, err := registryFor(kind); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "no_such_kind",
			"the published kinds are organisation, terms and authorization (§3)")
		return
	}
	version := 1
	if s := r.URL.Query().Get("version"); s != "" {
		v, err := atoiPositive(s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "version is not a positive number")
			return
		}
		version = v
	}
	pub, err := publicationOf(r.Context(), h.d.DB.Q(), kind, r.PathValue("id"), version)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_published",
			"this fact has not reached the registry; it may be pending, or it may be one that never publishes")
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "read publication", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pub)
}
