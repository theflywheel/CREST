package evidence

import "testing"

// g4_7: the reuse metric must tell "no reuse yet" apart from "no data yet",
// and must never fabricate a rate over an empty registry.

func TestReuseMetricOnAnEmptyRegistryReportsNilRateNotZero(t *testing.T) {
	out := reuseMetric(nil, 0)
	if out["totalClaimedParties"] != 0 || out["reusedParties"] != 0 {
		t.Fatalf("expected zero totals, got %+v", out)
	}
	if out["reuseRate"] != nil {
		t.Fatalf("an empty registry has no rate to report, got %v", out["reuseRate"])
	}
}

func TestReuseMetricCountsOnlyPartiesSpanningMoreThanOneContext(t *testing.T) {
	spread := []partyContextSpread{
		{PartyID: "p1", Contexts: 1},
		{PartyID: "p2", Contexts: 2},
		{PartyID: "p3", Contexts: 3},
	}
	out := reuseMetric(spread, 4)
	if out["totalClaimedParties"] != 3 {
		t.Fatalf("total = %v, want 3", out["totalClaimedParties"])
	}
	if out["reusedParties"] != 2 {
		t.Fatalf("reused = %v, want 2 (p2 and p3)", out["reusedParties"])
	}
	rate, ok := out["reuseRate"].(float64)
	if !ok {
		t.Fatalf("reuseRate should be a float64, got %T", out["reuseRate"])
	}
	if want := 2.0 / 3.0; rate != want {
		t.Fatalf("reuseRate = %v, want %v", rate, want)
	}
	if out["distinctContexts"] != 4 {
		t.Fatalf("distinctContexts = %v, want 4", out["distinctContexts"])
	}
}

func TestReuseMetricIsZeroWhenNobodyIsClaimedByMoreThanOneContext(t *testing.T) {
	spread := []partyContextSpread{{PartyID: "p1", Contexts: 1}, {PartyID: "p2", Contexts: 1}}
	out := reuseMetric(spread, 1)
	if out["reusedParties"] != 0 {
		t.Fatalf("reused = %v, want 0", out["reusedParties"])
	}
	if rate := out["reuseRate"].(float64); rate != 0 {
		t.Fatalf("reuseRate = %v, want 0", rate)
	}
}
