// Package strength is the function f: the one place a trust tier is decided.
//
// The rule this package exists to enforce is that a tier is never stored
// (Blueprint §6). A credential carries provenance facts; the tier is computed
// from those facts against the definition version the credential pinned, plus
// how that source is regarded *today*. Three consequences follow, and all three
// are the reason for the shape below:
//
//   - Re-assessing a compromised source downgrades every affected credential
//     instantly, with no reissuance. That is what SourceAssessment is for.
//   - Versioning a definition never strands old credentials: they resolve
//     against their pinned version, which is why the definition is a parameter
//     rather than something this package looks up.
//   - No attester can inflate strength by asserting it. Nothing an attester
//     controls is an input — not the transport, not any field the source
//     asserts about its own trustworthiness.
//
// f itself is L1 and identical in every deployment. The evidence-to-tier map it
// reads is L2 data inside each definition. Keeping the two apart is what makes
// "two deployments could disagree and both still be CREST" true here.
package strength

import (
	"fmt"
	"sort"

	"github.com/theflywheel/crest/pkg/schema"
)

// Facts are what the credential carries, plus what the deployment knows about
// the worker. Deliberately a small struct: if computing a tier ever needs a
// field that is not here, that is a design question, not a parameter to add.
type Facts struct {
	// Provenance as the adapter attached it. SourceExposure is present and is
	// never read — see TestTransportDoesNotAffectStrength.
	Provenance schema.Provenance

	// PresentFields is which of the definition's required fields the record
	// actually carried. The pipeline computes this; f does not go looking.
	PresentFields []string

	// IdentityAssurance is derived from the Party's bindings, never stored on
	// the Party (§4.1). Empty means IA-0.
	IdentityAssurance schema.IdentityAssurance
}

// SourceAssessment is the deployment's current standing for a source, keyed by
// the adapterRef that appears in provenance. Absent means "no concerns".
//
// This is the mechanism behind "re-assessing a compromised source system
// downgrades every affected credential's tier instantly". It is read at
// evaluation time, so the downgrade is a data change, not a migration.
type SourceAssessment struct {
	MaxTier int
	Reason  string
}

// Result is a judgement with its reasoning attached. The reasoning is not a
// nicety: §6 says the tier is always displayed to worker and verifier alike,
// and a number with no account of itself is not something either can argue with.
type Result struct {
	// Tier is 1–3 when Acceptable; zero otherwise.
	Tier int

	// Acceptable is false when no rule in the definition matched. That is not
	// "tier 0" — it is evidence this definition does not recognise at all.
	Acceptable bool

	// MatchedRule is the index of the winning rule in the definition's tierMap,
	// so a disagreement can be taken to the exact line of L2 data that caused it.
	MatchedRule int

	// Because explains the outcome in order: the rule that matched, then every
	// cap that reduced it. A tier that was capped reads differently from a tier
	// that was earned, and a worker is entitled to know which they have.
	Because []string
}

func (r Result) String() string {
	if !r.Acceptable {
		return "not acceptable: " + join(r.Because)
	}
	return fmt.Sprintf("tier %d (%s)", r.Tier, join(r.Because))
}

// Evaluate computes the tier for one set of facts against one definition
// version. It never writes anything, by construction: there is nowhere to.
func Evaluate(f Facts, def schema.Definition, assessment *SourceAssessment) Result {
	present := map[string]bool{}
	for _, name := range f.PresentFields {
		present[name] = true
	}

	for i, rule := range def.TierMap {
		if missing := unmet(f, rule, present); missing != "" {
			continue
		}
		res := Result{
			Tier:        rule.Tier,
			Acceptable:  true,
			MatchedRule: i,
			Because: []string{fmt.Sprintf("rule %d of the definition awards tier %d for %s evidence captured as %s",
				i, rule.Tier, f.Provenance.SourceClass, f.Provenance.CaptureMethod)},
		}

		// A definition may promise less than its own map could award. The
		// ceiling is on the worker face because it is a promise made to the
		// worker, and a worker should never be told a record is stronger than
		// the definition is willing to stand behind.
		if ceiling := def.Faces.Worker.TierCeiling; res.Tier > ceiling {
			res.Because = append(res.Because,
				fmt.Sprintf("capped to tier %d by the definition's ceiling", ceiling))
			res.Tier = ceiling
		}

		if assessment != nil && res.Tier > assessment.MaxTier {
			res.Because = append(res.Because,
				fmt.Sprintf("capped to tier %d by the current assessment of %s: %s",
					assessment.MaxTier, f.Provenance.AdapterRef, assessment.Reason))
			res.Tier = assessment.MaxTier
		}

		// A cap can take a tier below the floor. That is a real outcome — a
		// source assessed as untrustworthy produces evidence this definition
		// cannot recognise — and saying "tier 0" would invite someone to store
		// it as a number.
		if res.Tier < 1 {
			return Result{Acceptable: false, MatchedRule: i, Because: append(res.Because,
				"the caps leave nothing this definition recognises")}
		}
		return res
	}

	return Result{Acceptable: false, MatchedRule: -1, Because: []string{
		fmt.Sprintf("no rule in definition %s@%d matches %s evidence captured as %s with fields %v at %s",
			def.ID, def.Version, f.Provenance.SourceClass, f.Provenance.CaptureMethod,
			sorted(f.PresentFields), assurance(f.IdentityAssurance)),
	}}
}

// unmet returns the first condition of the rule the facts do not satisfy, or
// "" when the rule matches. Returning *which* condition failed is what makes a
// rule table debuggable rather than a black box.
func unmet(f Facts, rule schema.DefinitionTierMapItem, present map[string]bool) string {
	if !containsSourceClass(rule.SourceClassIn, f.Provenance.SourceClass) {
		return "sourceClass"
	}
	if !containsCaptureMethod(rule.CaptureMethodIn, f.Provenance.CaptureMethod) {
		return "captureMethod"
	}
	for _, field := range rule.RequiresFields {
		if !present[field] {
			return "field " + field
		}
	}
	if rule.MinIdentityAssurance != nil {
		if assuranceRank(f.IdentityAssurance) < assuranceRank(*rule.MinIdentityAssurance) {
			return "identityAssurance"
		}
	}
	return ""
}

// assuranceRank orders the levels. IA-0 is the zero value on purpose: a Party
// with no bindings is asserted, and absence should not read as an error.
func assuranceRank(ia schema.IdentityAssurance) int {
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

func assurance(ia schema.IdentityAssurance) schema.IdentityAssurance {
	if ia == "" {
		return schema.IdentityAssuranceIA0
	}
	return ia
}

func containsSourceClass(in []schema.SourceClass, v schema.SourceClass) bool {
	for _, c := range in {
		if c == v {
			return true
		}
	}
	return false
}

func containsCaptureMethod(in []schema.CaptureMethod, v schema.CaptureMethod) bool {
	for _, c := range in {
		if c == v {
			return true
		}
	}
	return false
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}
