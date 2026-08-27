// Package clock is the only place in CREST permitted to read wall-clock time.
//
// The confirmation window is seven days (Blueprint §11, W3). A test suite that
// waits seven days is not a test suite, so every component takes a Clock and the
// harness advances it. The forbidigo rule in .golangci.yml enforces that nothing
// else calls time.Now.
package clock

import (
	"sync"
	"time"
)

// Clock reads the current time. Production uses System; tests use Fake.
type Clock interface {
	Now() time.Time
}

// System reads real wall-clock time. Use it in main(), nowhere else.
type System struct{}

// Now returns the current UTC time.
func (System) Now() time.Time { return time.Now().UTC() } //nolint:forbidigo // the one permitted call

// Fake is a controllable clock. Safe for concurrent use, because the harness
// advances time from one goroutine while services read it from others.
type Fake struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFake starts a fake clock at t.
func NewFake(t time.Time) *Fake { return &Fake{now: t.UTC()} }

// Now returns the fake clock's current time.
func (f *Fake) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

// Advance moves the clock forward. Negative durations are allowed: time going
// backwards is a real condition worth testing, not one to pretend away.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the clock to an absolute instant.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

// Driveable is a clock the harness can move. Both Fake and Offset satisfy it;
// the difference is what happens when nobody is driving.
type Driveable interface {
	Clock
	Advance(d time.Duration)
	Set(t time.Time)
}

// Offset is a driveable clock that still ticks.
//
// Fake is right for a unit test, where nothing should move unless the test
// moves it. It is wrong for a running deployment: a demo stack seeded by
// walking a week of the programme forward ends with its clock frozen at the
// last step, and then nothing ever becomes due again — no window reaches T=7,
// no payment is released by the passage of time, no source heartbeat ages. The
// system looks alive and is not.
//
// Offset is the shape that serves both. It reads real time and adds a skew, so
// the seeder can put the clock a week in the past, walk the story forward at
// whatever pace it likes, and leave the deployment running on a clock that
// keeps moving on its own from wherever the story stopped.
type Offset struct {
	mu   sync.RWMutex
	base Clock
	skew time.Duration
}

// NewOffset builds an offset clock over base, starting with no skew.
func NewOffset(base Clock) *Offset { return &Offset{base: base} }

// NewOffsetAt builds an offset clock over base reading t right now.
func NewOffsetAt(base Clock, t time.Time) *Offset {
	o := NewOffset(base)
	o.Set(t)
	return o
}

// Now returns real time plus the current skew.
func (o *Offset) Now() time.Time {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.base.Now().Add(o.skew).UTC()
}

// Advance moves the clock forward by d, on top of whatever real time has done.
func (o *Offset) Advance(d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.skew += d
}

// Set moves the clock to t, by choosing the skew that makes Now() read t. Time
// carries on from there rather than stopping, which is the whole point.
func (o *Offset) Set(t time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.skew = t.UTC().Sub(o.base.Now())
}

// Skew is how far this clock is from real time. Reported at startup and by
// GET /internal/clock so a stack running on a shifted clock says so.
func (o *Offset) Skew() time.Duration {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.skew
}
