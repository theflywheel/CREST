//go:build e2e

package scenarios

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// The auto-confirm exit is the only one of the four that no person triggers.
// Every other scenario proves it by posting to /v1/sweep, which proves the
// logic and not the wiring — and on a running deployment nobody posts to
// /v1/sweep at all. This proves the loop: advance past T=7, touch nothing, and
// the window must exit and release payment on its own.
//
// Run by `make test-e2e-sweep`, which brings the stack up with SWEEP_EVERY set.
// The default compose stack leaves it off so the scenarios that ask for the
// sweep by hand are not racing a background one.
func TestScheduledSweepPaysWithNobodyAsking(t *testing.T) {
	every := os.Getenv("SWEEP_EVERY")
	if every == "" || every == "0" {
		t.Skip("SWEEP_EVERY is off on this stack; run make test-e2e-sweep")
	}
	interval, err := time.ParseDuration(every)
	if err != nil {
		t.Fatalf("SWEEP_EVERY=%q is not a duration", every)
	}

	w := setup(t)
	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerBID)
	result := w.submit(t, batch(row(phone, 3, "HH-SWEEP")))
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	if err := w.Advance(w.ctx, window+time.Minute); err != nil {
		t.Fatal(err)
	}

	// No POST /v1/sweep anywhere below. Waiting is the assertion.
	wait := 4*interval + 20*time.Second
	eventually(t, "the scheduled sweep auto-confirms the window", wait, func() error {
		win, err := w.window(claimID)
		if err != nil {
			return err
		}
		if win.ExitRoute == nil {
			return fmt.Errorf("still open")
		}
		if *win.ExitRoute != "auto" {
			return fmt.Errorf("exited by %q, want auto", *win.ExitRoute)
		}
		if win.PaymentReleasedAt == nil {
			return fmt.Errorf("auto-confirmed but released no payment (every T=7 exit releases payment)")
		}
		return nil
	})
}
