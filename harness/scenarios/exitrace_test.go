//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// A window that is due can be exited by two actors in the same instant: the
// sweep (auto) and the worker (self). Both exits are legitimate; what must
// never happen is both of them completing — two credentials signed for one
// claim, or the record of one exit overwriting the other. The exit path
// serialises on the window row, so whichever loses the race sees a closed
// window and returns the winner's result instead of writing its own.
func TestASweepAndAConfirmationRacingAtT7ExitOnce(t *testing.T) {
	w := setup(t)

	phone := sharedNumber(207)
	worker := newWorkerWithPhone(t, w, "Race Exiter", phone)
	result := w.submit(t, batch(row(phone, 2, "HH-RACE")))
	claimID := result.ClaimIDs[0]

	// The sweep only auto-confirms a reached worker, so wait for the
	// notification to land before making the window due.
	eventually(t, "the worker is reached", 20*time.Second, func() error {
		win, err := w.window(claimID)
		if err != nil {
			return err
		}
		if win.Reach == nil || *win.Reach != "reached" {
			return fmt.Errorf("reach is not yet recorded")
		}
		return nil
	})
	if err := w.Advance(w.ctx, window+time.Minute); err != nil {
		t.Fatal(err)
	}

	// Fire both exits from the same starting line.
	caller := w.login(t, worker)
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(2)
	var confirmErr, sweepErr error
	go func() {
		defer done.Done()
		start.Wait()
		confirmErr = w.Confirmation.As(caller).
			Post(w.ctx, "/v1/claims/"+claimID+"/confirm", map[string]any{"route": "self"}, nil)
	}()
	go func() {
		defer done.Done()
		start.Wait()
		sweepErr = w.Confirmation.Post(w.ctx, "/v1/sweep", nil, nil)
	}()
	start.Done()
	done.Wait()

	// Neither actor may see a failure: the loser's exit is idempotent.
	if confirmErr != nil {
		t.Errorf("the worker's confirmation failed in the race: %v", confirmErr)
	}
	if sweepErr != nil {
		t.Errorf("the sweep failed in the race: %v", sweepErr)
	}

	win, err := w.window(claimID)
	if err != nil {
		t.Fatal(err)
	}
	if win.ExitRoute == nil {
		t.Fatal("the window never exited")
	}
	if *win.ExitRoute != "self" && *win.ExitRoute != "auto" {
		t.Fatalf("exited by %q, want self or auto", *win.ExitRoute)
	}
	if win.PaymentReleasedAt == nil {
		t.Fatal("the race exited the window without releasing payment")
	}

	// Exactly one credential for the claim. Two would mean both racers won,
	// and revoking one worker's slot could then revoke a stranger's.
	var wallet struct {
		Credentials []json.RawMessage `json:"credentials"`
	}
	if err := w.Verification.As(caller).
		Get(w.ctx, "/v1/parties/"+worker+"/credentials", &wallet); err != nil {
		t.Fatal(err)
	}
	forClaim := 0
	for _, doc := range wallet.Credentials {
		if strings.Contains(string(doc), claimID) {
			forClaim++
		}
	}
	if forClaim != 1 {
		t.Fatalf("the race issued %d credentials for one claim, want exactly 1", forClaim)
	}

	// And the payment lands. The release rides the outbox, so give it a moment.
	eventually(t, "the payment instruction exists", 20*time.Second, func() error {
		_, err := w.instruction(claimID)
		return err
	})
}
