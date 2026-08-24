package strength_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/strength"
)

type vectorFile struct {
	Cases []struct {
		Name        string                         `json:"name"`
		TierCeiling *int                           `json:"tierCeiling"`
		TierMap     []schema.DefinitionTierMapItem `json:"tierMap"`
		Facts       vectorFacts                    `json:"facts"`
		Assessment  *struct {
			MaxTier int    `json:"maxTier"`
			Reason  string `json:"reason"`
		} `json:"assessment"`
		Want want `json:"want"`
	} `json:"cases"`

	TransportInvariance struct {
		Facts     vectorFacts             `json:"facts"`
		Exposures []schema.SourceExposure `json:"exposures"`
		Want      want                    `json:"want"`
	} `json:"transportInvariance"`
}

type vectorFacts struct {
	SourceClass       schema.SourceClass       `json:"sourceClass"`
	CaptureMethod     schema.CaptureMethod     `json:"captureMethod"`
	SourceExposure    schema.SourceExposure    `json:"sourceExposure"`
	PresentFields     []string                 `json:"presentFields"`
	IdentityAssurance schema.IdentityAssurance `json:"identityAssurance"`
}

type want struct {
	Acceptable      bool   `json:"acceptable"`
	Tier            int    `json:"tier"`
	MatchedRule     *int   `json:"matchedRule"`
	BecauseContains string `json:"becauseContains"`
}

func (f vectorFacts) facts() strength.Facts {
	return strength.Facts{
		Provenance: schema.Provenance{
			SourceClass:    f.SourceClass,
			CaptureMethod:  f.CaptureMethod,
			SourceExposure: f.SourceExposure,
			AdapterRef:     "fixture-adapter@1",
		},
		PresentFields:     f.PresentFields,
		IdentityAssurance: f.IdentityAssurance,
	}
}

func load(t *testing.T) vectorFile {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	if len(vf.Cases) == 0 {
		t.Fatal("no vectors")
	}
	return vf
}

// The vectors are the specification (#15). This test is the thing that makes
// them executable; a second implementation should be able to read the same file
// and agree.
func TestVectors(t *testing.T) {
	vf := load(t)
	base := fixtures.MustLoad().Definition()

	for _, c := range vf.Cases {
		t.Run(c.Name, func(t *testing.T) {
			def := base
			if c.TierCeiling != nil {
				def.Faces.Worker.TierCeiling = *c.TierCeiling
			}
			if len(c.TierMap) > 0 {
				def.TierMap = c.TierMap
			}
			var assessment *strength.SourceAssessment
			if c.Assessment != nil {
				assessment = &strength.SourceAssessment{
					MaxTier: c.Assessment.MaxTier,
					Reason:  c.Assessment.Reason,
				}
			}

			got := strength.Evaluate(c.Facts.facts(), def, assessment)

			if got.Acceptable != c.Want.Acceptable {
				t.Fatalf("acceptable = %v, want %v (%s)", got.Acceptable, c.Want.Acceptable, got)
			}
			if !c.Want.Acceptable {
				if got.Tier != 0 {
					t.Errorf("unacceptable evidence reported tier %d; it must carry no tier at all", got.Tier)
				}
				return
			}
			if got.Tier != c.Want.Tier {
				t.Errorf("tier = %d, want %d (%s)", got.Tier, c.Want.Tier, got)
			}
			if c.Want.MatchedRule != nil && got.MatchedRule != *c.Want.MatchedRule {
				t.Errorf("matched rule %d, want %d", got.MatchedRule, *c.Want.MatchedRule)
			}
			if c.Want.BecauseContains != "" && !strings.Contains(got.String(), c.Want.BecauseContains) {
				t.Errorf("reasoning %q does not mention %q — a capped tier that does not "+
					"say it was capped is a number nobody can argue with", got.String(), c.Want.BecauseContains)
			}
		})
	}
}

// "The four transports never influence strength" (§8) is a property, not an
// example: an attester who could raise a tier by choosing how to send the file
// would have found a way to assert strength.
func TestTransportDoesNotAffectStrength(t *testing.T) {
	vf := load(t)
	def := fixtures.MustLoad().Definition()

	var first strength.Result
	for i, exposure := range vf.TransportInvariance.Exposures {
		f := vf.TransportInvariance.Facts
		f.SourceExposure = exposure
		got := strength.Evaluate(f.facts(), def, nil)

		if got.Tier != vf.TransportInvariance.Want.Tier {
			t.Fatalf("%s: tier = %d, want %d", exposure, got.Tier, vf.TransportInvariance.Want.Tier)
		}
		if i == 0 {
			first = got
			continue
		}
		if got.Tier != first.Tier || got.MatchedRule != first.MatchedRule {
			t.Errorf("%s produced a different judgement than %s: transport must not be an input",
				exposure, vf.TransportInvariance.Exposures[0])
		}
	}
}

// Evaluate takes the definition as a parameter rather than looking one up, so
// that a credential pinned to v1 keeps resolving against v1 forever. This test
// is what stops someone from "helpfully" adding a lookup later.
func TestOldCredentialsResolveAgainstTheirPinnedVersion(t *testing.T) {
	v1 := fixtures.MustLoad().Definition()

	// v2 tightens the top rule. A credential issued under v1 must not notice.
	v2 := fixtures.MustLoad().Definition()
	v2.Version = 2
	v2.TierMap = v2.TierMap[1:] // tier 3 no longer awarded at all

	facts := strength.Facts{
		Provenance: schema.Provenance{
			SourceClass:    schema.SourceClassNationalSystem,
			CaptureMethod:  schema.CaptureMethodSystemOfRecord,
			SourceExposure: schema.SourceExposurePushAPI,
			AdapterRef:     "fixture-adapter@1",
		},
		PresentFields:     []string{"household_id", "beneficiary_count"},
		IdentityAssurance: schema.IdentityAssuranceIA3,
	}

	if got := strength.Evaluate(facts, v1, nil); got.Tier != 3 {
		t.Errorf("under v1: tier = %d, want 3", got.Tier)
	}
	if got := strength.Evaluate(facts, v2, nil); got.Tier != 2 {
		t.Errorf("under v2: tier = %d, want 2 — the same facts, a stricter definition", got.Tier)
	}
}

// The retroactive upgrade (§4.1, issue #15). Identity assurance is a separate
// axis from evidence, derived from the Party at query time rather than frozen
// into the record. So a worker whose identity binding improves later — a
// contact-verified party who subsequently anchors against a national identity —
// must see the tier of evidence already captured rise, with nothing reissued
// and nothing about the stored provenance touched.
//
// The vectors already contain both ends of this as separate cases; what they
// cannot state is that it is the *same* evidence in both. That is the claim
// worth a test, because the failure it guards against is someone storing the
// tier and leaving a worker permanently judged at the assurance they happened
// to have on the day the work was recorded.
func TestAssuranceImprovingLaterRaisesTheTierWithNoReissuance(t *testing.T) {
	def := fixtures.MustLoad().Definition()

	// One captured record. Everything here is a stored provenance fact.
	stored := schema.Provenance{
		SourceClass:    schema.SourceClassNationalSystem,
		CaptureMethod:  schema.CaptureMethodSystemOfRecord,
		SourceExposure: schema.SourceExposurePushAPI,
		AdapterRef:     "fixture-adapter@1",
	}
	fields := []string{"household_id", "beneficiary_count"}

	before := strength.Evaluate(strength.Facts{
		Provenance:        stored,
		PresentFields:     fields,
		IdentityAssurance: schema.IdentityAssuranceIA1,
	}, def, nil)

	after := strength.Evaluate(strength.Facts{
		Provenance:        stored,
		PresentFields:     fields,
		IdentityAssurance: schema.IdentityAssuranceIA3,
	}, def, nil)

	if !before.Acceptable || !after.Acceptable {
		t.Fatalf("both evaluations must be acceptable: before=%v after=%v", before, after)
	}
	if after.Tier <= before.Tier {
		t.Fatalf("assurance rose IA-1 -> IA-3 and the tier did not: before %s, after %s.\n"+
			"Evidence already captured must benefit from a later identity binding, or a "+
			"worker is judged forever at the assurance they had on the day they worked.",
			before, after)
	}
}
