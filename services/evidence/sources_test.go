package main

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)

func src(registered time.Time, lastSeen *time.Time, every time.Duration) *Source {
	return &Source{RegisteredAt: registered, LastSeenAt: lastSeen, expectedEvery: every}
}

// The state is derived, never stored — the same rule as the tier and a Party's
// assurance. This is the test that would fail if anyone cached it: the identical
// row reads HEALTHY and then SILENT with nothing but the clock moving.
func TestSourceStateIsDerivedFromTheClock(t *testing.T) {
	seen := t0
	s := src(t0.Add(-72*time.Hour), &seen, 24*time.Hour)

	s.stateAt(t0.Add(time.Hour))
	if s.State != SourceHealthy {
		t.Fatalf("an hour after a heartbeat, a daily source is %s", s.State)
	}
	s.stateAt(t0.Add(25 * time.Hour))
	if s.State != SourceSilent {
		t.Fatalf("25 hours after a heartbeat, a daily source is %s", s.State)
	}
	if s.QuietFor == "" {
		t.Error("a silent source does not say how long it has been quiet")
	}
}

// "Never sent anything" and "stopped sending" look identical in a count and
// need different people: one is a broken integration, the other is a deployment
// step nobody finished.
func TestNeverSeenIsNotTheSameAsSilent(t *testing.T) {
	s := src(t0, nil, 24*time.Hour)
	s.stateAt(t0.Add(time.Hour))
	if s.State != SourceNeverSeen {
		t.Fatalf("a registered source that has never sent is %s", s.State)
	}

	// It should not page anyone within its first cadence — a feed registered
	// this morning and due daily is not late yet.
	if s.overdue(t0.Add(time.Hour)) {
		t.Error("a source registered an hour ago on a daily cadence is already overdue")
	}
	// But it must not wait forever. A source registered a week ago that has
	// never sent a row is not waiting patiently, it is broken.
	s.stateAt(t0.Add(7 * 24 * time.Hour))
	if !s.overdue(t0.Add(7 * 24 * time.Hour)) {
		t.Error("a source that has never sent anything in a week is not counted as overdue")
	}
}

// The boundary is the declared cadence, not a multiple of it invented here. A
// deployment that says "daily" has said what late means.
func TestSilenceBeginsAtTheDeclaredCadence(t *testing.T) {
	seen := t0
	s := src(t0, &seen, 24*time.Hour)

	s.stateAt(t0.Add(24 * time.Hour))
	if s.State != SourceHealthy {
		t.Errorf("exactly at the cadence a source is %s; the deadline has not passed yet", s.State)
	}
	s.stateAt(t0.Add(24*time.Hour + time.Second))
	if s.State != SourceSilent {
		t.Errorf("a second past the cadence a source is %s", s.State)
	}
}
