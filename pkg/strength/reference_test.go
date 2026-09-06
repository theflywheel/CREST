package strength_test

import (
	"encoding/json"
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/strength"
)

func TestReferenceTierNumbersAndCaptureLimits(t *testing.T) {
	semantics := schema.DefinitionTierSemanticsReferenceV1
	def := schema.Definition{TierSemantics: &semantics}
	def.Faces.Worker.TierCeiling = 1
	for _, tc := range []struct {
		name    string
		source  schema.SourceClass
		capture schema.CaptureMethod
		want    int
	}{
		{"independent national record", schema.SourceClassNationalSystem, schema.CaptureMethodSystemOfRecord, 1},
		{"independent institution record", schema.SourceClassInstitutionalSystem, schema.CaptureMethodSystemOfRecord, 1},
		{"supervisor witness", schema.SourceClassProgrammeSystem, schema.CaptureMethodSupervisedManual, 2},
		{"worker assertion", schema.SourceClassSelfReported, schema.CaptureMethodDigitalCapture, 3},
		{"unwitnessed manual record", schema.SourceClassInstitutionalSystem, schema.CaptureMethodUnsupervisedManual, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def.TierMap = []schema.DefinitionTierMapItem{{Tier: 1, SourceClassIn: []schema.SourceClass{tc.source}, CaptureMethodIn: []schema.CaptureMethod{tc.capture}}}
			f := strength.Facts{Provenance: schema.Provenance{SourceClass: tc.source, CaptureMethod: tc.capture}, IdentityAssurance: schema.IdentityAssuranceIA3}
			got := strength.Evaluate(f, def, nil)
			if !got.Acceptable || got.Tier != tc.want {
				t.Fatalf("got %s want tier %d", got, tc.want)
			}
			assessed := strength.Evaluate(f, def, &strength.SourceAssessment{MaxTier: 3, Reason: "investigation"})
			if assessed.Tier != 3 {
				t.Fatalf("assessment did not weaken evidence: %s", assessed)
			}
			rejected := strength.Evaluate(f, def, &strength.SourceAssessment{MaxTier: 0})
			if rejected.Acceptable || rejected.Tier != 0 {
				t.Fatalf("withdrawn source was accepted: %s", rejected)
			}
		})
	}
}

func TestLegacyTierInterpretationPreservesHistoricalDefinition(t *testing.T) {
	def := schema.Definition{TierMap: []schema.DefinitionTierMapItem{{Tier: 3, SourceClassIn: []schema.SourceClass{schema.SourceClassNationalSystem}, CaptureMethodIn: []schema.CaptureMethod{schema.CaptureMethodSystemOfRecord}}}}
	def.Faces.Worker.TierCeiling = 3
	before, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	f := strength.Facts{Provenance: schema.Provenance{SourceClass: schema.SourceClassNationalSystem, CaptureMethod: schema.CaptureMethodSystemOfRecord}}
	got := strength.Evaluate(f, def, nil)
	after, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tier != 1 || string(before) != string(after) {
		t.Fatalf("historical compatibility failed: %s mutated=%v", got, string(before) != string(after))
	}
}
