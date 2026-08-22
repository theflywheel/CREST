package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

var epoch = time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

func TestFakeAdvancesThroughTheConfirmationWindow(t *testing.T) {
	c := clock.NewFake(epoch)
	if got := c.Now(); !got.Equal(epoch) {
		t.Fatalf("Now() = %v, want %v", got, epoch)
	}

	// The whole point: seven days must pass in microseconds.
	c.Advance(7 * 24 * time.Hour)

	want := epoch.Add(7 * 24 * time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after advancing 7d, Now() = %v, want %v", got, want)
	}
}

func TestFakeGoesBackwards(t *testing.T) {
	c := clock.NewFake(epoch)
	c.Advance(-time.Hour)
	if got := c.Now(); !got.Before(epoch) {
		t.Fatalf("Now() = %v, want a time before %v", got, epoch)
	}
}

func TestFakeIsConcurrencySafe(t *testing.T) {
	c := clock.NewFake(epoch)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); c.Advance(time.Second) }()
		go func() { defer wg.Done(); _ = c.Now() }()
	}
	wg.Wait()

	want := epoch.Add(50 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestSystemClockIsUTC(t *testing.T) {
	if loc := (clock.System{}).Now().Location(); loc != time.UTC {
		t.Fatalf("System clock location = %v, want UTC", loc)
	}
}
