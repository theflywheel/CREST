//go:build e2e

package scenarios

import (
	"fmt"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// The outbox's actual promise, tested by breaking the thing it protects
// against (#47), reshaped by #129.
//
// When the window and the instruction were two services, this test killed
// payments, confirmed while it was down, and SIGKILLed confirmation between
// the exit's commit and the release's delivery — a deterministic crash in the
// owed state. The merge collapsed that seam: the exit, the outbox entry and
// the instruction now live in one service and one database, so "the
// downstream is down while the entry is owed" cannot be staged from outside.
//
// What survives, and still deserves executing, is the promise itself: the
// release is enqueued in the same transaction as the exit, so a SIGKILL of
// the payments service immediately after the exit commits must never lose the
// payment — whichever side of the relay's delivery the kill lands on, the
// restart finds either a delivered instruction or an owed entry and finishes
// the job, exactly once. The kill races the in-process relay rather than
// deterministically beating it; both interleavings assert the property that
// matters (nothing lost, nothing doubled), and the losing interleaving is
// still a restart-recovery proof.
func TestAPaymentReleaseSurvivesThePaymentsServiceBeingKilled(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	result := w.submit(t, batch(row(phone, 9, "HH-outbox")))
	if len(result.ClaimIDs) != 1 {
		t.Fatalf("want one claim, got %d", len(result.ClaimIDs))
	}
	claimID := result.ClaimIDs[0]

	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	// The worker confirms: the exit and the owed release commit together.
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.confirmClaim(t, claimID,
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if exit.Credential.ID == "" {
		t.Fatal("confirming produced no credential")
	}

	// The crash. SIGKILL, so nothing gets to drain, flush or apologise — the
	// process is simply gone, as soon after the commit as a test can arrange.
	if err := harness.Kill(w.ctx, "payments"); err != nil {
		t.Fatalf("kill payments: %v", err)
	}
	if err := harness.Start(w.ctx, "payments"); err != nil {
		t.Fatalf("restart payments: %v", err)
	}
	if err := w.WaitReady(w.ctx, 90*time.Second); err != nil {
		t.Fatalf("the stack did not come back: %v", err)
	}

	// And the release lands anyway, carried by an entry that was written in the
	// same transaction as the exit and outlived the process that wrote it.
	eventually(t, "the payment owed before the crash is released after it", 90*time.Second, func() error {
		if _, err := w.instruction(claimID); err != nil {
			return err
		}
		win, err := w.window(claimID)
		if err != nil {
			return err
		}
		if win.PaymentReleasedAt == nil {
			return fmt.Errorf("the instruction exists but the window does not record a release")
		}
		return nil
	})

	// Exactly one. At-least-once delivery plus an idempotent consumer must come
	// out as once — a redelivery that pays twice is the mirror image of the bug
	// this whole mechanism exists to prevent, and it is worse, because nobody
	// reports being paid twice.
	var all struct {
		Instructions []struct {
			ClaimID string `json:"claimId"`
		} `json:"instructions"`
	}
	if err := w.Payments.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, "/v1/instructions", &all); err != nil {
		t.Fatalf("list instructions: %v", err)
	}
	n := 0
	for _, in := range all.Instructions {
		if in.ClaimID == claimID {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d payment instructions for one claim after a crash and redelivery, want 1", n)
	}
}
