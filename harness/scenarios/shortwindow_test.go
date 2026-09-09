//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
)

// The auto-confirm exit, end to end, with nobody asking for it.
//
// This scenario is what two used to be. `test-e2e-short-window` proved that
// the window's length is configuration by bringing up a second stack with
// CONFIRMATION_WINDOW in seconds; `test-e2e-sweep` proved that the scheduled
// sweep runs on its own by bringing up a third with SWEEP_EVERY set. Since the
// driveable clock was removed (ruled 2026-09-09) the default stack is that
// stack: the window is seconds long, the sweep is always on, and every other
// scenario in this package is already running against those settings. Two
// extra stacks proving what the default one now proves continuously would be
// two more things to keep in step.
//
// What it holds:
//   - the window's length is programme policy, not an infrastructure constant
//     (#127, #128). CONFIRMATION_WINDOW is L2 configuration, and a stack whose
//     window is a few seconds opens, waits, auto-confirms, issues and pays by
//     real time passing.
//   - the auto exit is the one no person triggers. There is no POST /v1/sweep
//     anywhere below, because there is none on a running deployment either —
//     asking for the sweep by hand proves the logic and not the wiring.
//   - it happens at the boundary and not before. A window that exited early is
//     a worker whose chance to object was shorter than they were promised.
//   - and, like every other exit, it releases payment.
func TestTheWindowAutoConfirmsAtTheBoundaryAndPays(t *testing.T) {
	w := setup(t)
	phone := sharedNumber(208)
	worker := newWorkerWithPhone(t, w, "Short Window", phone)
	w.consentOf(t, worker)
	result := w.submit(t, batch(row(phone, 2, "HH-SHORTWIN")))
	claimID := onlyClaim(t, result)

	// Notifications are dropped (#150): no reach verdict exists, and the sweep
	// auto-confirms on NULL reach. Wait only for the window itself.
	var opened winView
	eventually(t, "the window opens", 20*time.Second, func() error {
		var err error
		opened, err = w.window(claimID)
		return err
	})
	w.acknowledgeClaim(t, claimID)

	// Not before the boundary. Read back while the window is provably still
	// open — the margin is the stack's own window length, and the sweep runs
	// every SWEEP_EVERY, so an early exit would already be visible here.
	if time.Now().UTC().Before(opened.ClosesAt) {
		still, err := w.window(claimID)
		if err != nil {
			t.Fatal(err)
		}
		if still.ExitRoute != nil {
			t.Fatalf("the window exited via %q at %s, before it closes at %s; a worker's chance "+
				"to object ended early", *still.ExitRoute, time.Now().UTC(), still.ClosesAt)
		}
	}

	// Nothing is driven and nothing is asked. Real time passing is the whole
	// of the input, and the assertion is what the stack does with it.
	win := w.waitForTheWindowToExit(t, claimID, "auto")
	if win.ClosesAt.After(time.Now().UTC()) {
		t.Errorf("the window exited before %s, which is when it closes", win.ClosesAt)
	}

	// The exit issued: the worker's wallet holds a credential naming the
	// claim, and the rail was told what to pay.
	caller := w.login(t, worker)
	eventually(t, "the credential and the instruction exist",
		harness.Patience(harness.SweepEvery)+30*time.Second, func() error {
			var wallet struct {
				Credentials []json.RawMessage `json:"credentials"`
			}
			if err := w.Verification.As(caller).
				Get(w.ctx, "/v1/parties/"+worker+"/credentials", &wallet); err != nil {
				return err
			}
			found := false
			for _, doc := range wallet.Credentials {
				if strings.Contains(string(doc), claimID) {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("no credential names claim %s", claimID)
			}
			if _, err := w.instruction(claimID); err != nil {
				return fmt.Errorf("no payment instruction: %w", err)
			}
			return nil
		})
}
