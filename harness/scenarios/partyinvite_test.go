//go:build e2e

package scenarios

import (
	"net/http"
	"strings"
	"testing"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// The invitation in front of the first-login bootstrap (finding #123): an
// approved organisation creates a person's record and mints a code; the
// person's own login claims it, and from then on their token IS that party.
// Nothing here is seeded into place — the org acts through the same routes
// the console will, and the claimant is a stranger the registry has never
// seen. Refusals are asserted alongside, because an invitation nobody can be
// refused on is the unbounded bootstrap wearing a new name.
func TestAnInvitedPersonClaimsTheirRecordWithTheirOwnLogin(t *testing.T) {
	w := setup(t)
	// The seeder's own subjects, bound at seed time: "seed|custodian" is the
	// organisation, "seed|approver" the custodian person. Used directly so
	// the scenario holds on a stack that has been seeded more than once —
	// a fresh self-bind of a new subject is refused on a party already bound.
	orgToken, err := w.oidc.Token(w.ctx, "seed|custodian")
	if err != nil {
		t.Fatalf("mint the organisation's token: %v", err)
	}
	asOrg := w.Parties.As(harness.Caller{Token: orgToken})

	// The organisation creates the record. Nobody is bound to it.
	var person struct {
		ID string `json:"id"`
	}
	if err := asOrg.Post(w.ctx, "/v1/parties", map[string]any{
		"kind": "person", "displayName": "Invited Author " + runID,
		"contactRoutes": []map[string]any{{"kind": "email", "value": "author-" + runID + "@example.org"}},
	}, &person); err != nil {
		t.Fatalf("create the person's party: %v", err)
	}

	// And mints the invitation. The code comes back once.
	var inv struct {
		PartyID    string `json:"partyId"`
		InviteCode string `json:"inviteCode"`
		InvitedBy  string `json:"invitedBy"`
	}
	if err := asOrg.Post(w.ctx, "/v1/parties/"+person.ID+"/invitations", nil, &inv); err != nil {
		t.Fatalf("mint the invitation: %v", err)
	}
	if inv.PartyID != person.ID || inv.InviteCode == "" || inv.InvitedBy != fixtures.OrgID {
		t.Fatalf("invitation = %+v, want the person's id, a code, and the org as inviter", inv)
	}

	// The invited person signs in for the first time: a stranger to the
	// registry, holding the code.
	sub := "invited|" + runID
	token, err := w.oidc.Token(w.ctx, sub)
	if err != nil {
		t.Fatalf("mint the stranger's token: %v", err)
	}
	asStranger := w.Parties.As(harness.Caller{Token: token})
	var me struct {
		PartyID string `json:"partyId"`
	}
	if err := asStranger.Get(w.ctx, "/v1/auth/me", &me); err != nil {
		t.Fatalf("who am I, before claiming: %v", err)
	}
	if me.PartyID != "" {
		t.Fatalf("a stranger already resolved to %s before claiming anything", me.PartyID)
	}

	// A wrong code is unknown, and says so — not "forbidden".
	code, body, err := asStranger.Status(w.ctx, http.MethodPost, "/v1/party-invitations/claim",
		map[string]any{"code": "nope-" + runID, "provider": "mock-oidc", "providerClass": "generic-oidc"})
	if err != nil {
		t.Fatalf("wrong-code claim: %v", err)
	}
	if code != http.StatusNotFound || !strings.Contains(string(body), "invitation_unknown") {
		t.Fatalf("a wrong code answered %d %s, want 404 invitation_unknown", code, body)
	}

	// The real one binds the stranger's own subject to the invited party. The
	// code is accepted however it was typed.
	var claimed struct {
		PartyID           string `json:"partyId"`
		IdentityAssurance string `json:"identityAssurance"`
	}
	typed := strings.ToUpper(inv.InviteCode[:8]) + " " + inv.InviteCode[8:]
	if err := asStranger.Post(w.ctx, "/v1/party-invitations/claim",
		map[string]any{"code": typed, "provider": "mock-oidc", "providerClass": "generic-oidc"}, &claimed); err != nil {
		t.Fatalf("claim the invitation: %v", err)
	}
	if claimed.PartyID != person.ID {
		t.Fatalf("claim bound %s, want %s", claimed.PartyID, person.ID)
	}
	if err := asStranger.Get(w.ctx, "/v1/auth/me", &me); err != nil {
		t.Fatalf("who am I, after claiming: %v", err)
	}
	if me.PartyID != person.ID {
		t.Fatalf("after the claim the token resolves to %q, want %s", me.PartyID, person.ID)
	}

	// A person cannot invite — the one who just claimed their record has a
	// token that IS a party now, and still no standing: that belongs to an
	// approved organisation, or the operator.
	var other struct {
		ID string `json:"id"`
	}
	if err := asOrg.Post(w.ctx, "/v1/parties", map[string]any{
		"kind": "person", "displayName": "Never Invited " + runID,
		"contactRoutes": []map[string]any{{"kind": "phone", "value": "+15550100" + runID[:3]}},
	}, &other); err != nil {
		t.Fatalf("create a second party: %v", err)
	}
	code, body, err = asStranger.Status(w.ctx, http.MethodPost, "/v1/parties/"+other.ID+"/invitations", nil)
	if err != nil {
		t.Fatalf("person invite attempt: %v", err)
	}
	if code != http.StatusForbidden || !strings.Contains(string(body), "not_an_inviter") {
		t.Fatalf("a person inviting answered %d %s, want 403 not_an_inviter", code, body)
	}

	// Spent: a second stranger with the same code is refused as "claimed",
	// and the party stays the first claimant's.
	token2, err := w.oidc.Token(w.ctx, "latecomer|"+runID)
	if err != nil {
		t.Fatalf("mint a second stranger's token: %v", err)
	}
	code, body, err = w.Parties.As(harness.Caller{Token: token2}).Status(w.ctx,
		http.MethodPost, "/v1/party-invitations/claim",
		map[string]any{"code": inv.InviteCode, "provider": "mock-oidc", "providerClass": "generic-oidc"})
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if code != http.StatusConflict || !strings.Contains(string(body), "invitation_claimed") {
		t.Fatalf("a spent code answered %d %s, want 409 invitation_claimed", code, body)
	}

	// And a bound party cannot be re-invited: the door only ever opens once.
	code, body, err = asOrg.Status(w.ctx, http.MethodPost, "/v1/parties/"+person.ID+"/invitations", nil)
	if err != nil {
		t.Fatalf("re-invite attempt: %v", err)
	}
	if code != http.StatusConflict || !strings.Contains(string(body), "party_already_bound") {
		t.Fatalf("re-inviting a bound party answered %d %s, want 409 party_already_bound", code, body)
	}
}

// An organisation registration is authenticated at the open door. The caller
// is bound to the new organisation by the registration transaction, so the
// applicant can immediately use that same login for applicant-side reads.
func TestARegisteringOrganisationBindsItsAuthenticatedApplicant(t *testing.T) {
	w := setup(t)
	asApplicant := w.registrationApplicant(t, "self-claiming-org")

	var reg struct {
		Party struct {
			ID string `json:"id"`
		} `json:"party"`
	}
	if err := w.Parties.As(asApplicant).Post(w.ctx, "/v1/organisations", map[string]any{
		"displayName":   "Self-claiming Org " + runID,
		"contactRoutes": []map[string]any{{"kind": "email", "value": "org-" + runID + "@example.org"}},
	}, &reg); err != nil {
		t.Fatalf("register with authenticated applicant: %v", err)
	}
	var me struct {
		PartyID string `json:"partyId"`
	}
	if err := w.Parties.As(asApplicant).Get(w.ctx, "/v1/auth/me", &me); err != nil {
		t.Fatalf("who am I: %v", err)
	}
	if me.PartyID != reg.Party.ID {
		t.Fatalf("the applicant resolves to %q, want the organisation %s", me.PartyID, reg.Party.ID)
	}
}
