package main

import (
	"testing"

	"github.com/theflywheel/crest/services/core/attestation"
	"github.com/theflywheel/crest/services/core/definitions"
	"github.com/theflywheel/crest/services/core/evidence"
	"github.com/theflywheel/crest/services/core/parties"
	"github.com/theflywheel/crest/services/core/verification"
)

// The driveable clock is the payments application's harness surface, not the
// substrate's (#127). It used to be unconditional in pkg/service, which meant
// every infrastructure member could have its time moved by an HTTP call — and
// a deployment whose clock a caller can move is one where a worker's
// confirmation window can be closed early on them.
//
// This asserts the capability is opt-in and that the infrastructure members
// which have no window do not opt in. attestation is the one exception, named
// here rather than waived silently: the confirmation window and its sweep
// still live in that member, which is the residual gap #127 records. When the
// member moves to the payments application, this case flips to false and the
// mount in attestation.Service goes with it.
func TestOnlyTheWindowOwnerAsksForADriveableClock(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		want bool
	}{
		{"parties", parties.Service().ClockSeam != nil, false},
		{"definitions", definitions.Service().ClockSeam != nil, false},
		{"evidence", evidence.Service().ClockSeam != nil, false},
		{"verification", verification.Service().ClockSeam != nil, false},
		{"attestation", attestation.Service().ClockSeam != nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set != tc.want {
				t.Fatalf("%s ClockSeam set = %v, want %v", tc.name, tc.set, tc.want)
			}
		})
	}
}
