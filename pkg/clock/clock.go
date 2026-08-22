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
