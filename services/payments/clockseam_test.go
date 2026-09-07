package main

import (
	"testing"

	"github.com/theflywheel/crest/services/core/definitions"
	"github.com/theflywheel/crest/services/core/evidence"
	"github.com/theflywheel/crest/services/core/parties"
	"github.com/theflywheel/crest/services/core/verification"
	"github.com/theflywheel/crest/services/payments/attestation"
)

// The driveable clock is the payments application's harness surface, not the
// substrate's (#127). It used to be unconditional in pkg/service, which meant
// every infrastructure member could have its time moved by an HTTP call — and
// a deployment whose clock a caller can move is one where a worker's
// confirmation window can be closed early on them.
//
// This test lives here, in the payments application, because that is where the
// window now lives. It used to live in services/core and carry a named
// exception for the attestation member, which was the residual #127 recorded:
// the window sat in the infrastructure deployable, so core mounted the seam
// too. The member has moved, so there is no exception left to name.
//
// The assertion is exact rather than per-member: across every member wiring in
// the fleet, the seam is declared exactly once, and the one that declares it is
// the payments member. "Exactly one" is the property worth protecting. A second
// declaration would either be an infrastructure service quietly regaining
// driveable time, or two members of one process disagreeing about which clock
// the process runs on — and Compose refuses the latter at startup, which is a
// crash in a demo rather than a caught mistake here.
func TestOnlyTheWindowOwnerAsksForADriveableClock(t *testing.T) {
	wirings := []struct {
		name     string
		layer    string
		declares bool
	}{
		{"parties", "infrastructure", parties.Service().ClockSeam != nil},
		{"definitions", "infrastructure", definitions.Service().ClockSeam != nil},
		{"evidence", "infrastructure", evidence.Service().ClockSeam != nil},
		{"verification", "infrastructure", verification.Service().ClockSeam != nil},
		{"attestation", "payments application", attestation.Service().ClockSeam != nil},
		{"payments", "payments application", paymentsMember().ClockSeam != nil},
	}

	var declarers []string
	for _, w := range wirings {
		if w.declares {
			declarers = append(declarers, w.name)
		}
	}
	if len(declarers) != 1 {
		t.Fatalf("clock seam declared by %v; want exactly one declarer", declarers)
	}
	if declarers[0] != "payments" {
		t.Fatalf("clock seam declared by %q; the window owner is the payments member", declarers[0])
	}

	// And say it member by member, so a failure names which one changed
	// rather than only that the count moved.
	for _, w := range wirings {
		t.Run(w.name, func(t *testing.T) {
			want := w.name == "payments"
			if w.declares != want {
				t.Fatalf("%s (%s) ClockSeam set = %v, want %v", w.name, w.layer, w.declares, want)
			}
		})
	}
}
