package parties

// Recovery-contact nomination (w1_7) and the confirmer's own view (w4_1, w4_2).
//
// See 0014_recovery_contacts.sql for the ruling this implements: a nomination
// is routing, not eligibility. It answers "who should be asked when this
// worker loses their device", and it feeds the confirmer's pending-request
// view; it never widens who may confirm — that stays the 2-of-3 rule with its
// distinct-authority constraint, exactly as recovery.go enforces it.
//
// Delivery is honestly absent. w4_1 draws "a request arrives by SMS", and this
// deployment has no SMS channel: notifications were dropped with the notify
// service and that gap is held deliberately (Blueprint §16, #150). What exists
// is the record an SMS adapter would deliver from — the nomination, and
// GET /v1/recoveries?confirmerPartyId= as the confirmer's pull view — so a
// returning channel has true things to say. Which channel, and its message
// templates, are L2/L3; that the request/answer is recorded is L1.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// RecoveryContact is one live or revoked nomination.
type RecoveryContact struct {
	PartyID        string     `json:"partyId"`
	ContactPartyID string     `json:"contactPartyId"`
	NominatedBy    string     `json:"nominatedByPartyId"`
	NominatedAt    time.Time  `json:"nominatedAt"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
}

var (
	errSelfNomination = errors.New("a recovery contact is someone else")
	errNoContactNamed = errors.New("no contact named")
)

// nominationAdmissible is the pure rule: a nomination names somebody, and that
// somebody is not the worker. Everything else — the contact existing, the
// pair being new — is a fact about the store, checked where the store is.
func nominationAdmissible(partyID, contactPartyID string) error {
	if contactPartyID == "" {
		return errNoContactNamed
	}
	if contactPartyID == partyID {
		return errSelfNomination
	}
	return nil
}

func registerRecoveryContactRoutes(mux *http.ServeMux, d service.Deps) {
	h := &recoveryContactHandlers{d: d}
	mux.HandleFunc("POST /v1/parties/{id}/recovery-contacts", h.nominate)
	mux.HandleFunc("GET /v1/parties/{id}/recovery-contacts", h.list)
	mux.HandleFunc("POST /v1/parties/{id}/recovery-contacts/{contactId}/revoke", h.revoke)
}

type recoveryContactHandlers struct {
	d service.Deps
}

// nominate records that this worker trusts this person to vouch for them.
// The worker themselves, or someone permitted to act for them — nomination at
// assisted enrolment is the field agent writing the worker's stated choice.
func (h *recoveryContactHandlers) nominate(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	var body struct {
		ContactPartyID string `json:"contactPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	// ?contextId= names the programme an assisted nomination happens under, so
	// a context-scoped act-for-party grant (the only kind a field agent holds,
	// by design — an instance-wide one would be a grant to be anybody) can
	// satisfy the check, exactly as consent capture threads it. Without it the
	// comment above — the agent writing the worker's stated choice — was a
	// promise the authorization model could not actually honour; found by the
	// w1_4 end-to-end walk. The worker acting for themselves needs no context.
	if _, ok := identity.Authorize(w, r, h.d.Log, partyID, r.URL.Query().Get("contextId"),
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	switch err := nominationAdmissible(partyID, body.ContactPartyID); {
	case errors.Is(err, errNoContactNamed):
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"contactPartyId is required: a nomination names a person, never a phone number — "+
				"how the person is reached lives on their own contact routes")
		return
	case errors.Is(err, errSelfNomination):
		httpx.WriteError(w, http.StatusBadRequest, "self_nomination",
			"a recovery contact is someone else: the premise of a recovery is that "+
				"this worker cannot speak for themselves")
		return
	}
	caller := identity.From(r.Context())
	nominatedBy := caller.PartyID
	if nominatedBy == "" {
		// Enforcement off (a local stack): the worker named in the path is the
		// only name available.
		nominatedBy = partyID
	}
	rec := RecoveryContact{
		PartyID: partyID, ContactPartyID: body.ContactPartyID,
		NominatedBy: nominatedBy, NominatedAt: h.d.Clock.Now(),
	}
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if _, err := getParty(r.Context(), tx, partyID); err != nil {
			return err
		}
		// The contact must exist as a party. A nomination of nobody is a
		// recovery that can never be routed.
		if _, err := getParty(r.Context(), tx, rec.ContactPartyID); err != nil {
			return errNoSuchContact
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO recovery_contacts (party_id, contact_party_id, nominated_by, nominated_at)
			VALUES ($1, $2, $3, $4)`,
			rec.PartyID, rec.ContactPartyID, rec.NominatedBy, rec.NominatedAt)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such party")
		return
	case errors.Is(err, errNoSuchContact):
		httpx.WriteError(w, http.StatusNotFound, "no_such_contact",
			"the nominated contact is not a party on this deployment; enrol them first — "+
				"a nomination of nobody is a recovery that can never be routed")
		return
	case store.IsUniqueViolation(err):
		httpx.WriteError(w, http.StatusConflict, "already_nominated",
			"this person is already a live recovery contact for this worker")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "nominate recovery contact", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rec)
}

var errNoSuchContact = errors.New("no such contact party")

// list is the worker's own view of who they nominated, revocations included —
// history, not just the current roster.
func (h *recoveryContactHandlers) list(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	if _, ok := identity.Authorize(w, r, h.d.Log, partyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	out, err := listRecoveryContacts(r.Context(), h.d.DB.Q(), partyID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list recovery contacts", err)
		return
	}
	live := 0
	for _, c := range out {
		if c.RevokedAt == nil {
			live++
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": partyID, "contacts": out, "liveCount": live,
	})
}

// revoke marks a nomination ended. The row stays: who a worker trusted, and
// when they stopped, answers a later dispute about a recovery.
func (h *recoveryContactHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	if _, ok := identity.Authorize(w, r, h.d.Log, partyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	var affected int64
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		n, err := tx.Exec(r.Context(), `
			UPDATE recovery_contacts SET revoked_at = $3
			WHERE party_id = $1 AND contact_party_id = $2 AND revoked_at IS NULL`,
			partyID, r.PathValue("contactId"), h.d.Clock.Now())
		affected = n
		return err
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "revoke recovery contact", err)
		return
	}
	if affected == 0 {
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no live nomination of that contact for this worker")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func listRecoveryContacts(ctx context.Context, q store.Querier, partyID string) ([]RecoveryContact, error) {
	rows, err := q.Query(ctx, `
		SELECT party_id, contact_party_id, nominated_by, nominated_at, revoked_at
		FROM recovery_contacts WHERE party_id = $1 ORDER BY nominated_at, contact_party_id`, partyID)
	if err != nil {
		return nil, err
	}
	out, err := store.Collect(rows, func(r store.Row) (RecoveryContact, error) {
		var c RecoveryContact
		return c, r.Scan(&c.PartyID, &c.ContactPartyID, &c.NominatedBy, &c.NominatedAt, &c.RevokedAt)
	})
	if out == nil {
		out = []RecoveryContact{}
	}
	return out, err
}

// isLiveRecoveryContact answers whether contact currently stands nominated by
// the party a recovery is about — the confirmer's-view filter.
func isLiveRecoveryContact(ctx context.Context, q store.Querier, partyID, contactID string) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM recovery_contacts
		WHERE party_id = $1 AND contact_party_id = $2 AND revoked_at IS NULL`,
		partyID, contactID).Scan(&n)
	return n > 0, err
}
