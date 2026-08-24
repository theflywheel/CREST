package main

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

var bindingClock = time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

func worker() schema.Party {
	return schema.Party{
		ID:          "did:crest:party:WORKER",
		Kind:        schema.PartyKindPerson,
		DisplayName: "A field worker",
		CreatedAt:   bindingClock.AddDate(0, -3, 0),
		ContactRoutes: []schema.PartyContactRoutesItem{
			{Kind: schema.PartyContactRoutesItemKindPhone, Value: "+15550100011"},
		},
	}
}

// The retroactive upgrade, at the level the registry decides it (#15, #17).
//
// A worker enrolled in a field visit has a phone and nothing else. Months later
// they authenticate against a national system. Nothing about the work they did
// in between changes — and every record of it must now be judged at the
// assurance they actually have, because the alternative is a worker permanently
// marked weaker for having been enrolled by somebody with a clipboard.
func TestBindingAnAnchorLaterRaisesAssuranceWithNothingReissued(t *testing.T) {
	p := worker()

	// Unverified phone alone: a route we have, not a route anyone has proven.
	before, _ := assuranceOf(p, bindingClock)
	if before != schema.IdentityAssuranceIA0 {
		t.Fatalf("assurance before any binding = %s, want IA-0", before)
	}

	p.IdentityBindings = append(p.IdentityBindings, schema.PartyIdentityBindingsItem{
		Provider:      "esignet",
		ProviderClass: schema.PartyIdentityBindingsItemProviderClassEsignet,
		SubjectRef:    "psut-0001",
		AssertedAt:    bindingClock,
	})

	after, because := assuranceOf(p, bindingClock)
	if after != schema.IdentityAssuranceIA3 {
		t.Fatalf("assurance after an eSignet binding = %s, want IA-3 (%v)", after, because)
	}
	if len(because) == 0 {
		t.Error("the level changed and said nothing about why; a worker cannot argue with a number")
	}
}

// History is never rewritten. A superseded binding is a true statement about
// who we thought somebody was at the time, and it is what makes a later dispute
// about an attribution answerable at all.
func TestAnExpiredBindingIsNoLevelRatherThanALowerOne(t *testing.T) {
	p := worker()
	expired := bindingClock.AddDate(0, -1, 0)
	p.IdentityBindings = []schema.PartyIdentityBindingsItem{{
		Provider:      "esignet",
		ProviderClass: schema.PartyIdentityBindingsItemProviderClassEsignet,
		SubjectRef:    "psut-old",
		AssertedAt:    bindingClock.AddDate(0, -6, 0),
		ExpiresAt:     &expired,
	}}

	level, _ := assuranceOf(p, bindingClock)
	if level != schema.IdentityAssuranceIA0 {
		t.Errorf("an expired eSignet binding still reads %s; expiry means the claim is no longer "+
			"live, not that it was downgraded", level)
	}

	// And it is still there to be read.
	if len(p.IdentityBindings) != 1 {
		t.Error("the expired binding was removed; history is never rewritten (§4.1)")
	}
}

// A binding class must not be able to assert more than the check behind it
// actually did. Everything downstream — including whether a definition accepts
// a record at all — reads this number.
func TestEachProviderClassAssertsOnlyWhatItChecked(t *testing.T) {
	for class, want := range map[schema.PartyIdentityBindingsItemProviderClass]schema.IdentityAssurance{
		schema.PartyIdentityBindingsItemProviderClassEsignet:      schema.IdentityAssuranceIA3,
		schema.PartyIdentityBindingsItemProviderClassMosipIda:     schema.IdentityAssuranceIA3,
		schema.PartyIdentityBindingsItemProviderClassGenericOidc:  schema.IdentityAssuranceIA3,
		schema.PartyIdentityBindingsItemProviderClassDocumentSeen: schema.IdentityAssuranceIA2,
		schema.PartyIdentityBindingsItemProviderClassMobileOtp:    schema.IdentityAssuranceIA1,
		schema.PartyIdentityBindingsItemProviderClassAsserted:     schema.IdentityAssuranceIA0,
	} {
		if got := levelFor(class); got != want {
			t.Errorf("%s asserts %s, want %s", class, got, want)
		}
	}
}
