package harness

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// The durations the harness stack is configured with, in one place.
//
// There is no clock to drive any more (ruled 2026-09-09). A window a week long
// cannot be waited out, so the harness does not run a week-long window: it
// brings the stack up with the same code and a window of seconds, and then
// waits real time for the real behaviour. What is proven is the behaviour —
// that the window closes on its own, that every exit releases payment, that a
// source falls quiet and comes back — and the length is proven to be
// configuration by the fact that these settings are all it took to change it.
//
// Every value here is read from the environment, because the stack is
// configured from the same environment: the compose file and the Makefile pass
// these exact variables to the services, and the scenarios read them back so a
// deadline is always expressed in terms of the cadence the stack is actually
// running at. Change one in the Makefile and the scenarios follow.
var (
	// ConfirmationWindow is how long a claim stays open for objection.
	// Seven days in the CHW programme; seconds here.
	ConfirmationWindow = envDuration("CONFIRMATION_WINDOW", 30*time.Second)

	// SweepEvery is how often the payments application looks for windows that
	// have run out. Always on: on a running deployment nobody posts to
	// /v1/sweep, so a suite that only ever swept by hand proved the logic and
	// not the wiring.
	SweepEvery = envDuration("SWEEP_EVERY", time.Second)

	// SourceMonitorEvery is how often evidence checks whether a source has
	// gone quiet, and SourceCadence is the per-source `expectedEvery` the
	// scenarios register their sources with. A daily source is what a
	// programme configures; two seconds is what proves the same code.
	SourceMonitorEvery = envDuration("SOURCE_MONITOR_EVERY", time.Second)
	SourceCadence      = envDuration("HARNESS_SOURCE_CADENCE", 2*time.Second)

	// ReviewAfter is how long after an identity override its mandatory review
	// falls due. Ninety days in the programme.
	ReviewAfter = envDuration("CREST_RECOVERY_OVERRIDE_REVIEW", 3*time.Second)

	// SkewAlert is how far core and payments may disagree about the time
	// before the handoff is flagged (#221).
	SkewAlert = envDuration("CLOCK_SKEW_ALERT", time.Second)
)

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		panic(fmt.Sprintf("harness: %s=%q is not a positive duration; the stack and the "+
			"scenarios must agree about how long things take", key, v))
	}
	return d
}

// Patience is how long to wait for something whose cadence is d.
//
// Ten times the cadence, floored at five seconds. Generous on purpose: a
// deadline that is a tight multiple of the interval is a test that fails on a
// loaded CI runner and gets re-run rather than read, and a suite people re-run
// past is a suite that is not proving anything. Waiting longer costs seconds
// on the runs that pass and nothing at all on the runs that fail, because
// WaitFor returns the moment the condition holds.
func Patience(d time.Duration) time.Duration {
	if p := 10 * d; p > 5*time.Second {
		return p
	}
	return 5 * time.Second
}

// WaitFor polls cond until it returns nil, or fails the test at the deadline
// with the last thing cond complained about.
//
// This is the only shape a time-bound assertion takes in CREST. Never sleep a
// fixed guess and then assert: the guess is either too short on a slow machine
// — which is a flaky suite, and flakiness is a defect — or wasted on a fast
// one. Polling returns as soon as the thing has happened, so a generous
// deadline costs nothing when the code is right.
//
// The counterpart, asserting that something has NOT happened yet, is
// deliberately not a helper: it needs a margin the cadence guarantees, and
// that margin is a judgement each scenario has to make in the open.
func WaitFor(t *testing.T, what string, within time.Duration, cond func() error) {
	t.Helper()
	deadline := time.Now().Add(within)
	var last error
	for {
		if last = cond(); last == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: not true within %s: %v", what, within, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
