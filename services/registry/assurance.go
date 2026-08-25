package main

import (
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// assuranceOf derives a Party's identity assurance level from its bindings.
//
// Derived, never stored — the same rule as trust strength, for the same reason.
// A worker who binds an anchor next month must have that upgrade apply to
// credentials already issued (§4.1), and a stored level cannot do that without
// a migration nobody will run.
//
// The highest live binding wins. An expired binding is not a lower level, it is
// no level: it says what was true then, and this function answers what is true
// now.
func assuranceOf(p schema.Party, now time.Time) (schema.IdentityAssurance, []string) {
	best := schema.IdentityAssuranceIA0
	because := []string{"no live identity binding: asserted only"}

	for _, b := range p.IdentityBindings {
		if b.ExpiresAt != nil && b.ExpiresAt.Before(now) {
			continue
		}
		level := levelFor(b.ProviderClass)
		if rank(level) <= rank(best) {
			continue
		}
		best = level
		because = []string{fmt.Sprintf("%s binding with %s, asserted %s",
			b.ProviderClass, b.Provider, b.AssertedAt.Format(time.RFC3339))}
	}

	// A verified contact route is worth IA-1 on its own: it proves control of a
	// route, which is exactly what IA-1 claims and no more.
	if rank(best) < 1 {
		for _, r := range p.ContactRoutes {
			if r.VerifiedAt != nil && !r.VerifiedAt.After(now) {
				return schema.IdentityAssuranceIA1,
					[]string{fmt.Sprintf("%s route verified %s", r.Kind, r.VerifiedAt.Format(time.RFC3339))}
			}
		}
	}
	return best, because
}

// levelFor maps a provider class to what it can honestly assert. eSignet and
// MOSIP IDA authenticate against a national system, which is IA-3. A document
// seen by an agent is IA-2: checksum-valid, not authenticated online. OTP
// proves a route and nothing else.
func levelFor(class schema.PartyIdentityBindingsItemProviderClass) schema.IdentityAssurance {
	switch class {
	case schema.PartyIdentityBindingsItemProviderClassEsignet,
		schema.PartyIdentityBindingsItemProviderClassMosipIda,
		schema.PartyIdentityBindingsItemProviderClassGenericOidc:
		return schema.IdentityAssuranceIA3
	case schema.PartyIdentityBindingsItemProviderClassDocumentSeen:
		return schema.IdentityAssuranceIA2
	case schema.PartyIdentityBindingsItemProviderClassMobileOtp:
		return schema.IdentityAssuranceIA1
	case schema.PartyIdentityBindingsItemProviderClassRecovery:
		// Two people vouching — or one supervisor overriding — is community
		// knowledge, not a national identity check, and the level must not say
		// otherwise (§16, #106). IA-1 until the worker re-anchors with a real
		// provider, at which point the stronger binding simply wins above:
		// "drops until re-anchored" is not a stored penalty, it is what
		// deriving from the bindings naturally produces.
		return schema.IdentityAssuranceIA1
	default:
		return schema.IdentityAssuranceIA0
	}
}

func rank(ia schema.IdentityAssurance) int {
	switch ia {
	case schema.IdentityAssuranceIA3:
		return 3
	case schema.IdentityAssuranceIA2:
		return 2
	case schema.IdentityAssuranceIA1:
		return 1
	default:
		return 0
	}
}
