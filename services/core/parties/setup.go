package parties

import (
	"errors"
	"net/http"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

var errInstanceAlreadySetup = errors.New("this instance already has an operator")

func setupCallerAllowed(c identity.Caller, expectedSubject, expectedIssuer string) bool {
	return c.Authenticated() && c.PartyID == "" && expectedSubject != "" && expectedIssuer != "" &&
		c.Subject == expectedSubject && c.Issuer == expectedIssuer && !c.Assisting()
}

func (h *handlers) setupInstance(w http.ResponseWriter, r *http.Request) {
	caller := identity.From(r.Context())
	if !caller.Authenticated() {
		httpx.WriteError(w, http.StatusUnauthorized, "identity_required", "sign in before setting up the instance")
		return
	}
	if !setupCallerAllowed(caller, config.Str("CREST_SETUP_SUBJECT_REF", ""), config.Str("CREST_SETUP_ISSUER", "")) {
		httpx.WriteError(w, http.StatusForbidden, "not_setup_administrator", "this identity is not the configured first-run administrator, or has already been enrolled")
		return
	}
	inst, err := loadInstance(h.d.Config)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read instance configuration", err)
		return
	}
	var body struct {
		DisplayName   string                          `json:"displayName"`
		ContactRoutes []schema.PartyContactRoutesItem `json:"contactRoutes"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if len(body.ContactRoutes) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "contact_required", "the operating organisation must provide a contact route")
		return
	}
	now := h.d.Clock.Now()
	p := schema.Party{
		ID: inst.OperatorPartyID, Kind: schema.PartyKindOrganisation,
		DisplayName: body.DisplayName, ContactRoutes: body.ContactRoutes, CreatedAt: now,
		IdentityBindings: []schema.PartyIdentityBindingsItem{{
			Provider: caller.Issuer, ProviderClass: schema.PartyIdentityBindingsItemProviderClassGenericOidc,
			SubjectRef: caller.Subject, AssertedAt: now,
		}},
	}
	if err := schema.Validate(schema.IDParty, p); err != nil {
		writeValidation(w, err)
		return
	}
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('crest.instance.setup'))`); err != nil {
			return err
		}
		if _, err := getParty(r.Context(), tx, p.ID); err == nil {
			return errInstanceAlreadySetup
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		var initialized bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM instance_setup)`).Scan(&initialized); err != nil {
			return err
		}
		if initialized {
			return errInstanceAlreadySetup
		}
		if existing, err := partyForSubject(r.Context(), tx, caller.Subject); err != nil {
			return err
		} else if existing != "" {
			return errInstanceAlreadySetup
		}
		if err := insertParty(r.Context(), tx, p); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO instance_setup (instance_id,operator_party_id,administrator_subject,administrator_issuer,completed_at) VALUES ($1,$2,$3,$4,$5)`, inst.ID, p.ID, caller.Subject, caller.Issuer, now); err != nil {
			return err
		}
		// The configured administrator has no Party by design: setup is the
		// authenticated deployment trust-root decision, not a fabricated second
		// person. The private instance_setup row is the decision record. The
		// registration's self pointer is accepted only when this FK links it to
		// that record; ordinary registry approvals retain non-self approval.
		if _, err := tx.Exec(r.Context(), `INSERT INTO org_registrations (party_id,state,decided_by,decided_at,reason,applied_at,decision_source,setup_instance_id) VALUES ($1,'APPROVED',$1,$2,'Instance operator established by the configured administrator',$2,'INSTANCE_SETUP',$3)`, p.ID, now, inst.ID); err != nil {
			return err
		}
		return enqueueFact(r.Context(), tx, "organisation", p.ID, 1)
	})
	if errors.Is(err, errInstanceAlreadySetup) {
		httpx.WriteError(w, http.StatusConflict, "instance_initialized", "first-run setup is already complete")
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "set up instance", err)
		return
	}
	if h.d.ForgetSubject != nil {
		h.d.ForgetSubject(caller.Subject)
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"party": p, "instance": inst, "initialized": true})
}
