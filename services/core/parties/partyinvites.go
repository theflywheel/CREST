package parties

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Party invitations (§4.1, finding #123): how a person who is not a worker
// gets from "somebody created my record" to "I am signed in as it".
//
// The registry has one door for a first login: an unbound party accepts the
// self-proof of whoever holds a token, because holding the token is the one
// proof that needs no prior binding. That door has no invitation in front of
// it — anyone who learns an unbound id can walk through — and no console
// surface ever used it for staff, so every non-worker persona in this project
// has so far been bound by a seeder or by hand. Removing the seed means this
// door needs a front: a code minted by somebody with standing, shown once,
// single-use, expiring, recorded against the party with the inviter's name.
// The claim appends the claimant's own binding through the same append-only
// path as every other binding; nothing about assurance changes.
//
// Which rule could this break: verification. A binding raises assurance and
// assurance raises the tier of evidence already captured, so who may put a
// binding on a party is exactly the question #102 settled. An invitation
// never widens that — the party must be unbound (the same state the bare
// bootstrap already accepts), the claimant still binds only their own
// subject, and the claim is refused once the party carries any binding.

const (
	inviteDefaultTTL = 7 * 24 * time.Hour
	inviteMaxTTL     = 30 * 24 * time.Hour
)

var (
	errInviteUnknown  = errors.New("no such invitation")
	errInviteExpired  = errors.New("the invitation has expired")
	errInviteClaimed  = errors.New("the invitation was already claimed")
	errPartyBound     = errors.New("the party is already bound to an identity")
	errNotAnInviter   = errors.New("only the instance operator or an approved organisation may invite")
	errSubjectIsParty = errors.New("this identity is already bound to a party")
)

// partyInvitation is the recorded offer: who invited whom to claim which
// party, and whether it was taken. The code itself is never stored.
type partyInvitation struct {
	CodeHash       string
	PartyID        string
	InvitedBy      string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ClaimedAt      *time.Time
	ClaimedSubject *string
}

// newInviteCode mints the code that travels out of band: 120 bits from the
// system's entropy, base32 without padding so it survives a URL and a phone
// call. It is returned once and only its hash is kept.
func newInviteCode() (string, error) {
	var b [15]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
}

// inviteCodeHash is the stored form. Codes are high-entropy, so a plain hash
// is enough: there is nothing to rainbow. Case and whitespace are dropped
// first — a code read aloud arrives in any case and often in groups.
func inviteCodeHash(code string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(code), ""))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// inviteTTL bounds how long a code lives. A zero or negative request takes the
// default; nothing may exceed the cap, because an invitation that never
// expires is the unbounded bootstrap capability this table exists to retire.
func inviteTTL(requested time.Duration) time.Duration {
	if requested <= 0 {
		return inviteDefaultTTL
	}
	if requested > inviteMaxTTL {
		return inviteMaxTTL
	}
	return requested
}

// inviteAdmissible is the whole claim decision, as a pure function: the
// invitation must exist, be unexpired and unclaimed, and the party it names
// must still be unbound. Order matters for the message a person sees — an
// expired code says "expired", not "already bound".
func inviteAdmissible(inv *partyInvitation, party schema.Party, now time.Time) error {
	if inv == nil {
		return errInviteUnknown
	}
	if inv.ClaimedAt != nil {
		return errInviteClaimed
	}
	if !now.Before(inv.ExpiresAt) {
		return errInviteExpired
	}
	if len(party.IdentityBindings) != 0 {
		return errPartyBound
	}
	return nil
}

func insertInvitation(ctx context.Context, tx store.Querier, inv partyInvitation) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO party_invitations (code_hash, party_id, invited_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		inv.CodeHash, inv.PartyID, inv.InvitedBy, inv.CreatedAt, inv.ExpiresAt)
	return err
}

func getInvitationLocked(ctx context.Context, q store.Querier, codeHash string) (*partyInvitation, error) {
	var inv partyInvitation
	err := q.QueryRow(ctx, `
		SELECT code_hash, party_id, invited_by, created_at, expires_at, claimed_at, claimed_subject
		FROM party_invitations WHERE code_hash = $1 FOR UPDATE`, codeHash).
		Scan(&inv.CodeHash, &inv.PartyID, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt,
			&inv.ClaimedAt, &inv.ClaimedSubject)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func markClaimed(ctx context.Context, tx store.Querier, codeHash, subject string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE party_invitations SET claimed_at = $2, claimed_subject = $3 WHERE code_hash = $1`,
		codeHash, at, subject)
	return err
}

// mintInvitation creates the offer for a party inside the caller's
// transaction and returns the one-time code. Shared by the invite endpoint
// and organisation registration, which mints one for the organisation it
// just created so the applicant can claim it with their own login.
func mintInvitation(ctx context.Context, tx store.Querier, partyID, invitedBy string,
	now time.Time, ttl time.Duration) (string, error) {
	code, err := newInviteCode()
	if err != nil {
		return "", err
	}
	if err := insertInvitation(ctx, tx, partyInvitation{
		CodeHash:  inviteCodeHash(code),
		PartyID:   partyID,
		InvitedBy: invitedBy,
		CreatedAt: now,
		ExpiresAt: now.Add(inviteTTL(ttl)),
	}); err != nil {
		return "", err
	}
	return code, nil
}

// canInvite decides who may put an invitation in front of a party: the
// instance operator, or an APPROVED organisation — the same standing
// createAuthorization demands of an authority, and for the same reason. A
// party of the right shape is not an authority until the registry's decision
// says so; a person is never one.
func (h *handlers) canInvite(ctx context.Context, q store.Querier, actor string) error {
	if actor == "" {
		return errNotAnInviter
	}
	if inst, err := loadInstance(h.d.Config); err == nil && inst.OperatorPartyID != "" && inst.OperatorPartyID == actor {
		return nil
	}
	p, err := getParty(ctx, q, actor)
	if err != nil {
		return err
	}
	if p.Kind != schema.PartyKindOrganisation {
		return errNotAnInviter
	}
	reg, err := getRegistration(ctx, q, actor)
	if err != nil || reg.State != stateApproved {
		return errNotAnInviter
	}
	return nil
}

// createPartyInvitation — POST /v1/parties/{id}/invitations. The actor is the
// caller's own party, or the party they are proven to act for (a person
// signed in for their organisation, with the act-for-party grant). The target
// must exist and must be unbound: an invitation to a party somebody already
// holds is not an invitation, it is a takeover.
func (h *handlers) createPartyInvitation(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	var body struct {
		ExpiresInHours int `json:"expiresInHours"`
	}
	if r.ContentLength != 0 && !httpx.ReadJSON(w, r, &body) {
		return
	}
	caller := identity.From(r.Context())
	actor, err := identity.Acting(r.Context(), caller, r.URL.Query().Get("contextId"), h.d.Permits)
	if err != nil {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted_to_act_for", "%v", err)
		return
	}
	if !h.d.Authenticating && actor == "" {
		// An unenforced stack (unit and local runs without a provider) has no
		// caller identity to record; the invitation still needs an inviter.
		actor = caller.PartyID
	}
	partyID := r.PathValue("id")
	var code string
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := h.canInvite(r.Context(), tx, actor); err != nil {
			return err
		}
		p, err := getPartyLocked(r.Context(), tx, partyID, true)
		if err != nil {
			return err
		}
		if len(p.IdentityBindings) != 0 {
			return errPartyBound
		}
		code, err = mintInvitation(r.Context(), tx, partyID, actor, h.d.Clock.Now(),
			time.Duration(body.ExpiresInHours)*time.Hour)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such party")
	case errors.Is(err, errNotAnInviter):
		httpx.WriteError(w, http.StatusForbidden, "not_an_inviter", "%v", err)
	case errors.Is(err, errPartyBound):
		httpx.WriteError(w, http.StatusConflict, "party_already_bound", "%v", err)
	case err != nil:
		httpx.Fail(w, h.d.Log, "create party invitation", err)
	default:
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"partyId":    partyID,
			"inviteCode": code,
			"invitedBy":  actor,
			"expiresAt":  h.d.Clock.Now().Add(inviteTTL(time.Duration(body.ExpiresInHours) * time.Hour)),
		})
	}
}

// claimPartyInvitation — POST /v1/party-invitations/claim. The caller is an
// authenticated stranger holding a code; the claim appends THEIR subject to
// the invited party. Exactly the bootstrap self-bind, with an invitation in
// front of it: the party must still be unbound, the code unspent and alive,
// and the subject not already somebody else — appendBinding refuses that
// last case itself, because one login must never be two people.
func (h *handlers) claimPartyInvitation(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	var body struct {
		Code          string `json:"code"`
		Provider      string `json:"provider"`
		ProviderClass string `json:"providerClass"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	caller := identity.From(r.Context())
	if body.Code == "" || body.Provider == "" || body.ProviderClass == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"code, provider and providerClass are required")
		return
	}
	if caller.PartyID != "" {
		httpx.WriteError(w, http.StatusConflict, "already_enrolled", "%v", errSubjectIsParty)
		return
	}
	b := schema.PartyIdentityBindingsItem{
		Provider:      body.Provider,
		ProviderClass: schema.PartyIdentityBindingsItemProviderClass(body.ProviderClass),
		SubjectRef:    caller.Subject,
		AssertedAt:    h.d.Clock.Now(),
	}
	var (
		party    schema.Party
		appended bool
	)
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		inv, err := getInvitationLocked(r.Context(), tx, inviteCodeHash(body.Code))
		if err != nil {
			return err
		}
		if inv == nil {
			return errInviteUnknown
		}
		p, err := getPartyLocked(r.Context(), tx, inv.PartyID, true)
		if err != nil {
			return err
		}
		if err := inviteAdmissible(inv, p, h.d.Clock.Now()); err != nil {
			return err
		}
		party, appended, err = appendBinding(r.Context(), tx, inv.PartyID, b)
		if err != nil {
			return err
		}
		return markClaimed(r.Context(), tx, inv.CodeHash, caller.Subject, h.d.Clock.Now())
	})
	if err == nil && appended && h.d.ForgetSubject != nil && caller.Subject != "" {
		h.d.ForgetSubject(caller.Subject)
	}
	switch {
	case errors.Is(err, errInviteUnknown):
		httpx.WriteError(w, http.StatusNotFound, "invitation_unknown", "%v", err)
	case errors.Is(err, errInviteExpired):
		httpx.WriteError(w, http.StatusGone, "invitation_expired", "%v", err)
	case errors.Is(err, errInviteClaimed):
		httpx.WriteError(w, http.StatusConflict, "invitation_claimed", "%v", err)
	case errors.Is(err, errPartyBound):
		httpx.WriteError(w, http.StatusConflict, "party_already_bound", "%v", err)
	case errors.Is(err, ErrBindingBelongsToAnother):
		httpx.WriteError(w, http.StatusConflict, "identifier_belongs_to_another_party",
			"this identity already resolves to a different party")
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "the invited party no longer exists")
	case err != nil:
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "claim party invitation", err)
	default:
		level, because := assuranceOf(party, h.d.Clock.Now())
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"partyId":           party.ID,
			"bindings":          party.IdentityBindings,
			"identityAssurance": level,
			"because":           because,
		})
	}
}

// BootstrapOperator is the deploy-time act tools/bootstrap-operator performs:
// write the operator's party and mint the invitation its first login claims.
// It is the only party a clean registry cannot obtain through a door, because
// the doors answer to it. Exported for the tool and nothing else; a service
// route never calls it.
func BootstrapOperator(ctx context.Context, db *store.DB, p schema.Party, ttl time.Duration) (string, error) {
	if p.Kind != schema.PartyKindOrganisation {
		return "", errors.New("the operator must be an organisation")
	}
	var code string
	err := db.InTx(ctx, func(tx store.Querier) error {
		if err := insertParty(ctx, tx, p); err != nil {
			return err
		}
		var err error
		code, err = mintInvitation(ctx, tx, p.ID, p.ID, p.CreatedAt, ttl)
		return err
	})
	return code, err
}
