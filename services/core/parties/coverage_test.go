package parties

import "testing"

// g4_4: coverage-by-place is a headline about registration, not a fabricated
// percentage — it must never invent a denominator CREST does not own.

func TestCoverageByPlaceCountsEachPlaceOnce(t *testing.T) {
	got := coverageByPlace([]string{"Turkana North", "Turkana North", "Nairobi"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 places, got %d: %+v", len(got), got)
	}
	byPlace := map[string]int{}
	for _, row := range got {
		byPlace[row.Place] = row.Registered
		if row.Estimate != nil || row.Coverage != nil {
			t.Errorf("no denominator was supplied; %q must carry no estimate or coverage", row.Place)
		}
	}
	if byPlace["Turkana North"] != 2 || byPlace["Nairobi"] != 1 {
		t.Fatalf("wrong counts: %+v", byPlace)
	}
}

func TestCoverageByPlaceSurfacesUnspecifiedRatherThanDropping(t *testing.T) {
	got := coverageByPlace([]string{"Nairobi", "", "  "}, nil)
	var unspecified int
	for _, row := range got {
		if row.Place == "" {
			unspecified = row.Registered
		}
	}
	// "  " trims to "" too — a blank attribute is still unspecified.
	if unspecified != 2 {
		t.Fatalf("expected the two blank entries to land in the unspecified bucket, got %d", unspecified)
	}
	if got[len(got)-1].Place != "" {
		t.Fatal("the unspecified bucket must sort last, not alongside real places")
	}
}

func TestCoverageByPlaceJoinsADenominatorOnlyWhenOneWasGiven(t *testing.T) {
	got := coverageByPlace(
		[]string{"Turkana North", "Turkana North", "Nairobi"},
		map[string]int{"Turkana North": 1108},
	)
	var turkana, nairobi placeCount
	for _, row := range got {
		switch row.Place {
		case "Turkana North":
			turkana = row
		case "Nairobi":
			nairobi = row
		}
	}
	if turkana.Estimate == nil || *turkana.Estimate != 1108 {
		t.Fatalf("Turkana North should carry the supplied estimate, got %+v", turkana)
	}
	if turkana.Coverage == nil {
		t.Fatal("Turkana North should carry a computed coverage percentage")
	}
	want := 2.0 / 1108.0 * 100
	if *turkana.Coverage != want {
		t.Fatalf("coverage percentage = %v, want %v", *turkana.Coverage, want)
	}
	if nairobi.Estimate != nil || nairobi.Coverage != nil {
		t.Fatal("Nairobi had no denominator supplied and must carry none")
	}
}

func TestCoverageByPlaceHandlesNoPartiesAtAll(t *testing.T) {
	got := coverageByPlace(nil, map[string]int{"Turkana North": 1108})
	if len(got) != 0 {
		t.Fatalf("an empty registry must report zero buckets, not a fabricated 0%%, got %+v", got)
	}
}

func TestCoverageByPlaceOmitsCoverageForAZeroDenominator(t *testing.T) {
	// A deployment could pass 0 by mistake; a division by it must not happen.
	got := coverageByPlace([]string{"Nairobi"}, map[string]int{"Nairobi": 0})
	if got[0].Estimate == nil || *got[0].Estimate != 0 {
		t.Fatalf("the supplied (zero) estimate should still be echoed back, got %+v", got[0])
	}
	if got[0].Coverage != nil {
		t.Fatal("a zero denominator must not produce a coverage percentage")
	}
}
