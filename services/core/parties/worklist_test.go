package parties

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// g4_5: the worklist is a row per named gap, never a completeness score.

func TestQualityGapsNamesEveryMissingThing(t *testing.T) {
	// HasOpenHold is deliberately true here: unlike the binding and consent
	// facts, "no open hold" is the good state at zero-value, so a party with
	// every named gap has to say so explicitly.
	gaps := qualityGaps(partyGapInputs{HasOpenHold: true})
	if len(gaps) != 3 {
		t.Fatalf("a party with nothing on file should carry all three gaps, got %+v", gaps)
	}
	kinds := map[string]bool{}
	for _, g := range gaps {
		kinds[g.Kind] = true
		if g.FixableBy == "" {
			t.Errorf("gap %q must name who can fix it", g.Kind)
		}
	}
	for _, want := range []string{gapNoIdentityBinding, gapNoEnrolmentConsent, gapUnresolvedHold} {
		if !kinds[want] {
			t.Errorf("missing gap kind %q", want)
		}
	}
}

func TestQualityGapsIsEmptyForACleanParty(t *testing.T) {
	gaps := qualityGaps(partyGapInputs{
		HasIdentityBinding:  true,
		HasEnrolmentConsent: true,
		HasOpenHold:         false,
	})
	if len(gaps) != 0 {
		t.Fatalf("a party with every fact present must have no gaps, got %+v", gaps)
	}
}

func TestQualityGapsIsPartial(t *testing.T) {
	gaps := qualityGaps(partyGapInputs{HasIdentityBinding: true, HasEnrolmentConsent: false, HasOpenHold: true})
	if len(gaps) != 2 {
		t.Fatalf("expected exactly the consent and hold gaps, got %+v", gaps)
	}
}

func TestBuildWorklistDropsPartiesWithNoGap(t *testing.T) {
	parties := []schema.Party{
		{ID: "p1", DisplayName: "Clean Party"},
		{ID: "p2", DisplayName: "Gappy Party"},
	}
	inputs := map[string]partyGapInputs{
		"p1": {HasIdentityBinding: true, HasEnrolmentConsent: true, HasOpenHold: false},
		"p2": {},
	}
	rows := buildWorklist(parties, inputs)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one row (the gappy party), got %+v", rows)
	}
	if rows[0].PartyID != "p2" {
		t.Fatalf("wrong party surfaced: %+v", rows[0])
	}
}

func TestBuildWorklistIsEmptyForAnEmptyRegistry(t *testing.T) {
	rows := buildWorklist(nil, nil)
	if len(rows) != 0 {
		t.Fatalf("no parties, no rows; got %+v", rows)
	}
}

// A party absent from the inputs map (the zero value) is read as missing its
// binding and consent facts — buildWorklist must not skip a party it has no
// computed facts for, rather than silently treating "no entry" as "clean".
func TestBuildWorklistTreatsAMissingInputAsUncomputedNotClean(t *testing.T) {
	parties := []schema.Party{{ID: "p1", DisplayName: "Uncomputed", CreatedAt: time.Now()}}
	rows := buildWorklist(parties, map[string]partyGapInputs{})
	if len(rows) != 1 || len(rows[0].Gaps) != 2 {
		t.Fatalf("expected one row with the binding and consent gaps, got %+v", rows)
	}
}
