//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The window's length is programme policy, not an infrastructure constant
// (#127, #128) — CONFIRMATION_WINDOW is L2 configuration. This proves the
// configuration is real: a stack whose window is a few seconds long must open,
// auto-confirm, issue and release payment by real time passing, with the
// driveable clock never touched. It is the existence proof for #129's larger
// claim that the infrastructure can lose clock control entirely: nothing here
// needs a clock anyone can drive.
//
// Run by `make test-e2e-short-window`, which brings the stack up with
// CONFIRMATION_WINDOW and SWEEP_EVERY in seconds. On the default stack (168h)
// this skips.
func TestAShortWindowPaysByRealTimeAlone(t *testing.T) {
	raw := os.Getenv("CONFIRMATION_WINDOW")
	short, err := time.ParseDuration(raw)
	if raw == "" || err != nil || short > time.Minute {
		t.Skipf("CONFIRMATION_WINDOW=%q is not a short window; run make test-e2e-short-window", raw)
	}
	every, err := time.ParseDuration(os.Getenv("SWEEP_EVERY"))
	if err != nil || every == 0 {
		t.Skip("SWEEP_EVERY is off on this stack; run make test-e2e-short-window")
	}

	w := setup(t)
	phone := sharedNumber(208)
	worker := newWorkerWithPhone(t, w, "Short Window", phone)
	result := w.submit(t, batch(row(phone, 2, "HH-SHORTWIN")))
	claimID := result.ClaimIDs[0]

	// The sweep only auto-confirms a reached worker, so the notification has
	// to land inside the window — with seconds on the clock, that is itself
	// part of what is being proven.
	// Notifications are dropped (#150): no reach verdict exists, and the
	// sweep auto-confirms on NULL reach. Wait only for the window itself.
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	// No Advance, no SetClock, no POST /v1/sweep. The wall clock is the only
	// thing that moves, and it must be enough.
	wait := short + 4*every + 20*time.Second
	eventually(t, "the window auto-confirms by real time alone", wait, func() error {
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
			return fmt.Errorf("auto-confirmed but released no payment (every exit releases payment)")
		}
		return nil
	})

	// The exit issued: the worker's wallet holds a credential naming the
	// claim, and the rail was told what to pay.
	caller := w.login(t, worker)
	eventually(t, "the credential and the instruction exist", 30*time.Second, func() error {
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
