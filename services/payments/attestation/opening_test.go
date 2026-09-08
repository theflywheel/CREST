package attestation

import (
	"testing"
	"time"
)

// Where a confirmation window's opening instant comes from (#221).
//
// A truth table, so it is a unit test: the rule is a function of two instants,
// and running a stack to learn what a subtraction does would be testing Docker.
// The E2E scenarios in harness/scenarios/ prove the same rule survives the two
// processes, the outbox and a delivery that arrives late.

var (
	stamped = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	window  = 7 * 24 * time.Hour
	alert   = 5 * time.Minute
)

func TestTheWindowOpensWhenTheWorkerCouldFirstHaveSeenTheRecord(t *testing.T) {
	// The delivery arrives four hours after evidence stamped it — a retry
	// through an outage, or a slow queue. The worker is owed seven days from
	// when they could first have seen the record, so the window is already four
	// hours old when it opens, and it closes at the instant it always would
	// have.
	arrived := stamped.Add(4 * time.Hour)

	open := decideOpening(stamped, arrived)
	if !open.At.Equal(stamped) {
		t.Fatalf("the window opened at %s, want the instant evidence supplied (%s)", open.At, stamped)
	}
	if !open.Supplied {
		t.Fatal("a supplied instant was not recorded as supplied, so nothing would ever detect skew")
	}
	if got := closesFrom(open.At, window); !got.Equal(stamped.Add(window)) {
		t.Fatalf("the window closes at %s, want seven days after the worker could first have seen "+
			"the record (%s); anything later is a deadline this deployment moved", got, stamped.Add(window))
	}
}

// The fallback, for exactly one deploy: a core that has not yet learned to send
// the instant. It is the pre-#221 behaviour, and openWindow logs that it ran.
func TestAHandoffWithNoInstantFallsBackToTheArrivalClock(t *testing.T) {
	arrived := stamped.Add(4 * time.Hour)

	open := decideOpening(time.Time{}, arrived)
	if !open.At.Equal(arrived) {
		t.Fatalf("with nothing supplied the window must open on this process's clock, got %s", open.At)
	}
	if open.Supplied {
		t.Fatal("the fallback reported itself as a supplied instant, so the log would not say it ran")
	}
	if open.Delta != 0 {
		t.Fatalf("there is no second clock to compare against, so the delta must be zero, got %s", open.Delta)
	}
	if !open.exceeds(alert) {
		return // as it must: nothing was supplied, so nothing disagreed
	}
	t.Fatal("the fallback raised a skew alert against a clock that never spoke")
}

func TestSkewIsMeasuredInBothDirections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrived time.Time
		want    bool
	}{
		{"payments an hour ahead of evidence", stamped.Add(time.Hour), true},
		{"payments an hour behind evidence", stamped.Add(-time.Hour), true},
		{"the two agree", stamped, false},
		{"a difference inside the threshold", stamped.Add(alert - time.Second), false},
		{"exactly the threshold", stamped.Add(alert), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			open := decideOpening(stamped, tc.arrived)
			if got := open.exceeds(alert); got != tc.want {
				t.Fatalf("exceeds(%s) = %v, want %v (delta %s)", alert, got, tc.want, open.Delta)
			}
			// Whatever the disagreement, the window still opens where evidence
			// said. Payments correcting the instant with its own clock would be
			// payments reading its own clock, which is the bug.
			if !open.At.Equal(stamped) {
				t.Fatalf("skew moved the opening instant to %s", open.At)
			}
		})
	}
}

// A threshold of zero switches the detector off rather than alerting on every
// message. Nobody should configure that, but a deployment that does gets a
// quiet detector rather than a log nobody can read.
func TestAZeroThresholdRaisesNothing(t *testing.T) {
	if decideOpening(stamped, stamped.Add(time.Hour)).exceeds(0) {
		t.Fatal("a zero threshold alerted")
	}
}

func TestTheSkewCounterIsWhatMakesTheDetectorReadable(t *testing.T) {
	var c skewCounter
	c.observe(decideOpening(stamped, stamped.Add(time.Hour)), alert)    // over
	c.observe(decideOpening(stamped, stamped.Add(-2*time.Hour)), alert) // over, the other way
	c.observe(decideOpening(stamped, stamped.Add(time.Second)), alert)  // under
	c.observe(decideOpening(time.Time{}, stamped), alert)               // fell back

	events, fallbacks, worst := c.snapshot()
	if events != 2 {
		t.Errorf("counted %v skew events, want 2", events)
	}
	if fallbacks != 1 {
		t.Errorf("counted %v fallbacks, want 1", fallbacks)
	}
	if worst != 2*time.Hour {
		t.Errorf("worst skew = %s, want 2h — the sign is not the size", worst)
	}
}
