package parties

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

var inviteNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func openInvitation(expiresIn time.Duration) *partyInvitation {
	return &partyInvitation{
		CodeHash:  inviteCodeHash("example"),
		PartyID:   "did:crest:party:INVITED",
		InvitedBy: "did:crest:party:ORG",
		CreatedAt: inviteNow.Add(-time.Hour),
		ExpiresAt: inviteNow.Add(expiresIn),
	}
}

// The code that travels out of band: enough entropy that guessing is not a
// path, and an alphabet that survives a URL and being read over a phone.
func TestInviteCodeIsLongLowercaseBase32(t *testing.T) {
	code, err := newInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z2-7]{24}$`).MatchString(code) {
		t.Fatalf("code %q is not 24 chars of lowercase base32", code)
	}
	again, _ := newInviteCode()
	if again == code {
		t.Fatal("two codes minted in a row were identical")
	}
}

// Only the hash is stored; the same code must hash the same however a
// person types it, because a code read aloud arrives in any case.
func TestInviteCodeHashIsCaseAndSpaceInsensitive(t *testing.T) {
	if inviteCodeHash("ABCD efgh") != inviteCodeHash(" abcdefgh ") {
		t.Fatal("hash differs on case or surrounding space")
	}
	if inviteCodeHash("abcdefgh") == inviteCodeHash("abcdefgi") {
		t.Fatal("two different codes hashed the same")
	}
}

// An invitation that never expires is the unbounded bootstrap capability this
// table exists to retire, so the lifetime is bounded from both ends.
func TestInviteTTLIsDefaultedAndCapped(t *testing.T) {
	if got := inviteTTL(0); got != inviteDefaultTTL {
		t.Fatalf("zero ttl = %v, want the default %v", got, inviteDefaultTTL)
	}
	if got := inviteTTL(-time.Hour); got != inviteDefaultTTL {
		t.Fatalf("negative ttl = %v, want the default", got)
	}
	if got := inviteTTL(2000 * time.Hour); got != inviteMaxTTL {
		t.Fatalf("oversized ttl = %v, want the cap %v", got, inviteMaxTTL)
	}
	if got := inviteTTL(2 * time.Hour); got != 2*time.Hour {
		t.Fatalf("in-range ttl = %v, want it unchanged", got)
	}
}

// The claim decision, refusal by refusal. Each case names the one fact that
// refuses it, because a person holding a code needs to be told which.
func TestInviteAdmissibleRefusesEachWayForItsOwnReason(t *testing.T) {
	unbound := worker()
	bound := worker()
	bound.IdentityBindings = []schema.PartyIdentityBindingsItem{{
		Provider: "esignet", ProviderClass: schema.PartyIdentityBindingsItemProviderClassEsignet,
		SubjectRef: "psut-someone-else", AssertedAt: inviteNow.Add(-time.Hour),
	}}
	claimedAt := inviteNow.Add(-time.Minute)
	spent := openInvitation(time.Hour)
	spent.ClaimedAt = &claimedAt

	cases := []struct {
		name  string
		inv   *partyInvitation
		party schema.Party
		want  error
	}{
		{"unknown code", nil, unbound, errInviteUnknown},
		{"already claimed", spent, unbound, errInviteClaimed},
		{"expired", openInvitation(-time.Second), unbound, errInviteExpired},
		{"expires exactly now", openInvitation(0), unbound, errInviteExpired},
		{"party bound meanwhile", openInvitation(time.Hour), bound, errPartyBound},
		{"open, alive, party unbound", openInvitation(time.Hour), unbound, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := inviteAdmissible(c.inv, c.party, inviteNow)
			if c.want == nil {
				if got != nil {
					t.Fatalf("inviteAdmissible = %v, want admitted", got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Fatalf("inviteAdmissible = %v, want %v", got, c.want)
			}
		})
	}
}

// A claimed invitation stays refused even after the party is bound by it:
// the ordering above puts "claimed" before "bound", so a second attempt with
// the same code says the invitation is spent rather than blaming the party.
func TestASpentInvitationSaysSpentNotBound(t *testing.T) {
	claimedAt := inviteNow.Add(-time.Minute)
	spent := openInvitation(time.Hour)
	spent.ClaimedAt = &claimedAt
	p := worker()
	p.IdentityBindings = []schema.PartyIdentityBindingsItem{{
		Provider: "esignet", ProviderClass: schema.PartyIdentityBindingsItemProviderClassEsignet,
		SubjectRef: "psut-claimant", AssertedAt: claimedAt,
	}}
	if got := inviteAdmissible(spent, p, inviteNow); !errors.Is(got, errInviteClaimed) {
		t.Fatalf("got %v, want %v", got, errInviteClaimed)
	}
}

// invitedOrg is the record the instance operator writes at g1_5: the
// reference's five fields, and nothing resembling an identity document.
func invitedOrg() schema.Party {
	return schema.Party{
		Kind:        schema.PartyKindOrganisation,
		DisplayName: "Ministry of Health",
		CreatedAt:   inviteNow,
		Attributes: map[string]any{
			"kind":          "Delivery organisation",
			"contactPerson": "Dr. Grace Wanjiru",
			"contactRole":   "Principal Secretary",
		},
		ContactRoutes: []schema.PartyContactRoutesItem{
			{Kind: schema.PartyContactRoutesItemKindEmail, Value: "g.wanjiru@health.go.ke"},
		},
	}
}

// The invitation addresses a record, not a person (#185, ruled 2026-09-07) —
// but it must still record WHOM it was handed to, because delivery is "shown
// once, out of band" (#150) and the row is the only account of that.
func TestAnInstanceInvitationRecordsWhoItWasAddressedTo(t *testing.T) {
	noSignatory := invitedOrg()
	noSignatory.Attributes = map[string]any{"kind": "Delivery organisation"}
	blankSignatory := invitedOrg()
	blankSignatory.Attributes["contactPerson"] = "   "
	noEmail := invitedOrg()
	noEmail.ContactRoutes = []schema.PartyContactRoutesItem{
		{Kind: schema.PartyContactRoutesItemKindPhone, Value: "+254700000000"},
	}
	blankEmail := invitedOrg()
	blankEmail.ContactRoutes = []schema.PartyContactRoutesItem{
		{Kind: schema.PartyContactRoutesItemKindEmail, Value: " "},
	}

	cases := []struct {
		name  string
		party schema.Party
		want  error
	}{
		{"the reference's five fields", invitedOrg(), nil},
		{"no signatory named", noSignatory, errNoSignatory},
		{"signatory is whitespace", blankSignatory, errNoSignatory},
		{"reachable, but not by email", noEmail, errNoWorkEmail},
		{"work email is whitespace", blankEmail, errNoWorkEmail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := instanceInvitationAddressed(c.party)
			if c.want == nil {
				if got != nil {
					t.Fatalf("instanceInvitationAddressed = %v, want admitted", got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Fatalf("instanceInvitationAddressed = %v, want %v", got, c.want)
			}
			// Every refusal reaches the caller under its own name, so an
			// operator is told which field is missing rather than "422".
			if want := map[error]string{
				errNoSignatory: "signatory_required",
				errNoWorkEmail: "work_email_required",
			}[c.want]; invitationRefusal(got) != want {
				t.Fatalf("refusal code = %q, want %q", invitationRefusal(got), want)
			}
		})
	}
}
