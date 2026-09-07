package main

import (
	"sort"
	"testing"

	"github.com/theflywheel/crest/services/core/definitions"
	"github.com/theflywheel/crest/services/core/evidence"
	"github.com/theflywheel/crest/services/core/parties"
	"github.com/theflywheel/crest/services/core/verification"
	"github.com/theflywheel/crest/services/payments/attestation"
)

// The driveable clock is opt-in, and this is the whole list of who opts in.
//
// It used to be unconditional in pkg/service, which meant every service
// carried a route that could move its own time — and a deployment whose clock
// a caller can move is one where a worker's confirmation window can be closed
// early on them. #213 made it a seam a service declares for itself.
//
// The question this test then answered was "does only the window owner
// declare it?", and the answer turned out to be no. Moving the window to the
// payments application (#127) left `evidence` and `parties` unable to test
// behaviour that has nothing to do with a window, and #215 ruled on it
// (2026-09-07): the seam is a NON-PRODUCTION HARNESS SURFACE. pkg/clockctl
// refuses it outside local/test and pkg/service's deployment refusal refuses
// it again, so a service declaring it leaks no programme policy into the
// substrate. What #127 forbade is the confirmation WINDOW living in the
// infrastructure, and no window does.
//
// So the property worth protecting is not "one declarer" but "exactly these
// declarers, each for a stated reason". A new declarer appearing without a
// reason is the regression — that is a service quietly acquiring the ability
// to move its own time — and a declarer disappearing silently takes a
// scheduled behaviour out of reach of the suite, which is how #215 was found
// in the first place.
func TestOnlyServicesWithScheduledBehaviourDeclareTheDriveableClock(t *testing.T) {
	wirings := []struct {
		name     string
		layer    string
		declares bool

		// why is the scheduled behaviour that cannot be tested without moving
		// time. Empty means this service has none and must not declare the
		// seam.
		why string
	}{
		{
			name: "payments", layer: "payments application",
			declares: paymentsMember().ClockSeam != nil,
			why:      "the confirmation window: seven days in the CHW programme, and its auto-confirm exit at T=7 (#127)",
		},
		{
			name: "attestation", layer: "payments application",
			declares: attestation.Service().ClockSeam != nil,
			why:      "", // it holds the window, but payments declares the seam once for the process
		},
		{
			name: "evidence", layer: "infrastructure",
			declares: evidence.Service().ClockSeam != nil,
			why:      "the source-quiet monitor: a daily feed that goes 25 hours without a batch is SILENT (#22, #215)",
		},
		{
			name: "parties", layer: "infrastructure",
			declares: parties.Service().ClockSeam != nil,
			why:      "an identity override past its review date has to appear on the review list (#215)",
		},
		{
			name: "definitions", layer: "infrastructure",
			declares: definitions.Service().ClockSeam != nil,
			why:      "",
		},
		{
			name: "verification", layer: "infrastructure",
			declares: verification.Service().ClockSeam != nil,
			why:      "",
		},
	}

	var got, want []string
	for _, w := range wirings {
		if w.declares {
			got = append(got, w.name)
		}
		if w.why != "" {
			want = append(want, w.name)
		}
	}
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("clock seam declared by %v, want exactly %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("clock seam declared by %v, want exactly %v", got, want)
		}
	}

	// Member by member, so a failure names which one changed and — when a
	// service has acquired the seam it should not have — says what the seam is
	// for, rather than only that a boolean moved.
	for _, w := range wirings {
		t.Run(w.name, func(t *testing.T) {
			switch {
			case w.why != "" && !w.declares:
				t.Fatalf("%s (%s) declares no clock seam, but it has scheduled behaviour that needs one: %s",
					w.name, w.layer, w.why)
			case w.why == "" && w.declares:
				t.Fatalf("%s (%s) declares a clock seam with no scheduled behaviour named for it. "+
					"A service that can move its own time needs a reason written down (#215); "+
					"add it here, or take the declaration out", w.name, w.layer)
			}
		})
	}

	// attestation is the one worth stating outright. It holds the confirmation
	// window and still declares nothing, because a process has one clock and
	// payments declares it once for both members — Compose refuses two members
	// asking for different seams rather than picking one.
	if attestation.Service().ClockSeam != nil {
		t.Fatal("attestation declares its own seam; the payments process declares one clock for both its members")
	}
}
