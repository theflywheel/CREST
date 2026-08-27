package clock_test

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// stub is a base clock a test can move by hand, so these tests assert on the
// offset's arithmetic rather than on how fast the machine ran them.
type stub struct{ at time.Time }

func (s *stub) Now() time.Time { return s.at }

func TestOffsetTicksWithItsBase(t *testing.T) {
	base := &stub{at: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)}
	c := clock.NewOffset(base)

	if got := c.Now(); !got.Equal(base.at) {
		t.Fatalf("with no skew the offset should read real time: got %s want %s", got, base.at)
	}
	base.at = base.at.Add(90 * time.Minute)
	if got := c.Now(); !got.Equal(base.at) {
		t.Fatalf("the offset must move with real time: got %s want %s", got, base.at)
	}
}

func TestOffsetSetLeavesTimeRunning(t *testing.T) {
	base := &stub{at: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)}
	c := clock.NewOffset(base)

	// The seeder's move: put the clock a week and a bit in the past.
	epoch := base.at.Add(-8 * 24 * time.Hour)
	c.Set(epoch)
	if got := c.Now(); !got.Equal(epoch) {
		t.Fatalf("Set should make Now read the instant it was given: got %s want %s", got, epoch)
	}

	// This is the property a Fake does not have, and the reason the deployed
	// demo had a clock that could never make anything due: after being set,
	// time carries on.
	base.at = base.at.Add(time.Hour)
	if got := c.Now(); !got.Equal(epoch.Add(time.Hour)) {
		t.Fatalf("a set clock must keep ticking: got %s want %s", got, epoch.Add(time.Hour))
	}
}

func TestOffsetAdvanceStacksOnRealTime(t *testing.T) {
	base := &stub{at: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)}
	c := clock.NewOffset(base)

	c.Advance(7 * 24 * time.Hour)
	base.at = base.at.Add(2 * time.Hour)

	want := time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("advance and real time should both count: got %s want %s", got, want)
	}
	if got := c.Skew(); got != 7*24*time.Hour {
		t.Fatalf("skew should report the drive, not the passage of time: got %s", got)
	}
}

func TestOffsetSatisfiesDriveable(t *testing.T) {
	var _ clock.Driveable = clock.NewOffset(clock.System{})
	var _ clock.Driveable = clock.NewFake(time.Now()) //nolint:forbidigo // a test's own reading
}
