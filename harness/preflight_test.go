package harness

import (
	"testing"
	"time"
)

// ComparePreflight is pure — expected config in, actual service reports in,
// mismatches out — so it is exercised directly, with no Docker and no stack
// (#79).

func TestComparePreflightAgreesOnNothingToReport(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "abc123"},
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true},
	}

	if got := ComparePreflight(expected, actual); len(got) != 0 {
		t.Fatalf("want no mismatches, got %v", got)
	}
}

func TestComparePreflightCatchesTheDeDiNodeLeftFromAPreviousRun(t *testing.T) {
	// This is #79's own account: a stack started against the deployed DeDi
	// node, and a run that expects the Postgres fallback.
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "dedi", Revision: "abc123"},
		"payments": {Transparency: "dedi", Revision: "abc123", HasClock: true, ClockTicking: true},
	}

	got := ComparePreflight(expected, actual)
	if len(got) != 2 {
		t.Fatalf("want one mismatch per service, got %v", got)
	}
	for _, m := range got {
		if m.Field != "transparency substrate" {
			t.Fatalf("mismatch field = %q, want transparency substrate", m.Field)
		}
		if m.Expected != "postgres" || m.Actual != "dedi" {
			t.Fatalf("mismatch = %+v, want expected=postgres actual=dedi", m)
		}
	}
}

func TestComparePreflightCatchesAFrozenNonDriveableClock(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: false},
	}

	got := ComparePreflight(expected, actual)
	if len(got) != 1 || got[0].Field != "clock mode" {
		t.Fatalf("want one clock-mode mismatch, got %v", got)
	}
}

// A service that declares no clock seam has no /internal/clock at all (#213),
// and that is design rather than fault. The preflight must not read "this
// service has no driveable clock" as a stale environment — core answered none
// for as long as #127's move left it without one, and a deployment that runs
// only the substrate answers none today.
func TestComparePreflightAcceptsAServiceWithNoClockAtAll(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		// core: no seam, so no route, so HasClock false and ClockTicking
		// meaningless. The zero value is the honest report.
		"core":     {Transparency: "postgres", Revision: "abc123"},
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true},
	}

	if got := ComparePreflight(expected, actual); len(got) != 0 {
		t.Fatalf("core having no clock is #127's design, not a stale stack; got %v", got)
	}
}

// The other half of the same rule. Accepting "no clock" per service must not
// quietly accept a stack where nothing at all can be driven — that is a stack
// where every window scenario hangs at T=0 and blames itself.
func TestComparePreflightCatchesAStackWhereNoProcessOwnsTheClock(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "abc123"},
		"payments": {Transparency: "postgres", Revision: "abc123"},
	}

	got := ComparePreflight(expected, actual)
	if len(got) != 1 || got[0].Field != "clock seam" {
		t.Fatalf("want one clock-seam mismatch naming the stack, got %v", got)
	}
}

// The confirmation window's opening instant is stamped by core and honoured by
// payments (#221). A stack whose two processes disagree about the time moves
// every worker's deadline, and every window-crossing scenario then asserts a
// number that is quietly wrong — which is the "environment vs. defect"
// confusion #79 exists to end, one layer down.
func TestComparePreflightCatchesTwoProcessesOnDifferentClocks(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	noon := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true, Now: noon},
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true, Now: noon.Add(2 * time.Hour)},
	}

	got := ComparePreflight(expected, actual)
	if len(got) != 1 || got[0].Field != "clock skew" {
		t.Fatalf("want one clock-skew mismatch naming the stack, got %v", got)
	}
	if !contains(got[0].Actual, "payments") || !contains(got[0].Actual, "core") {
		t.Fatalf("the mismatch must name both processes, got %q", got[0].Actual)
	}
}

// The threshold is generous on purpose: the statuses are gathered one service
// at a time over HTTP, and a stack that is a second apart is the gathering, not
// a drifting deployment. A suite that fails on that is a suite people re-run.
func TestComparePreflightToleratesTheTimeGatheringTakesAgainstTheThreshold(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	noon := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "abc123", Now: noon},
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true, Now: noon.Add(ClockSkewThreshold - time.Second)},
	}

	if got := ComparePreflight(expected, actual); len(got) != 0 {
		t.Fatalf("a difference inside the threshold is not a stale stack; got %v", got)
	}
}

// A service that reported no time says nothing about skew. Guessing from one
// clock is how a check starts failing runs for a reason it cannot substantiate.
func TestComparePreflightSaysNothingAboutSkewWithOnlyOneReportedTime(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "abc123"},
		"payments": {Transparency: "postgres", Revision: "abc123", HasClock: true, ClockTicking: true, Now: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)},
	}

	if got := ComparePreflight(expected, actual); len(got) != 0 {
		t.Fatalf("one clock cannot disagree with itself; got %v", got)
	}
}

func TestComparePreflightCatchesAStaleServiceLeftFromAPreviousBuild(t *testing.T) {
	expected := ExpectedConfig{Transparency: "postgres"}
	actual := map[string]ServiceStatus{
		"core":     {Transparency: "postgres", Revision: "old-build"},
		"payments": {Transparency: "postgres", Revision: "new-build", HasClock: true, ClockTicking: true},
	}

	got := ComparePreflight(expected, actual)
	var revisionMismatches int
	for _, m := range got {
		if m.Field == "build revision" {
			revisionMismatches++
		}
	}
	if revisionMismatches != 2 {
		t.Fatalf("want a build-revision mismatch named for both services, got %v", got)
	}
}

func TestComparePreflightIgnoresAnUnsetExpectation(t *testing.T) {
	// A zero-value ExpectedConfig (empty Transparency) is "no opinion", not
	// "expect empty" — otherwise a caller who forgot to set the field would
	// fail every single service on a mismatch nobody intended to assert.
	expected := ExpectedConfig{}
	actual := map[string]ServiceStatus{
		"core": {Transparency: "dedi", Revision: "abc123", HasClock: true, ClockTicking: true},
	}

	if got := ComparePreflight(expected, actual); len(got) != 0 {
		t.Fatalf("want no mismatches with an unset expectation, got %v", got)
	}
}

func TestStaleEnvironmentErrorNamesTheMismatch(t *testing.T) {
	err := &StaleEnvironmentError{Mismatches: []Mismatch{
		{Service: "core", Field: "transparency substrate", Expected: "postgres", Actual: "dedi"},
	}}
	msg := err.Error()
	if !contains(msg, "stale environment") {
		t.Fatalf("error message must say \"stale environment\", got: %s", msg)
	}
	if !contains(msg, "postgres") || !contains(msg, "dedi") {
		t.Fatalf("error message must name expected and actual, got: %s", msg)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
