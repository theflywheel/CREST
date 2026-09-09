//go:build e2e

package scenarios

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// Where a confirmation window's opening instant comes from (#221, ruled
// 2026-09-08).
//
// The window is opened by one process and its opening instant is stamped by
// another. Until #221 payments read its own clock when the handoff landed,
// which meant two entirely ordinary things moved a worker's deadline with no
// symptom anywhere: a delivery retried through an outage, and skew between the
// two processes' clocks.
//
// These scenarios used to reproduce that by driving the payments clock away
// from core's. There is no clock to drive since 2026-09-09, and skewing two
// containers' system clocks against each other is not something a test may do
// — it would leave the machine, not the stack, in a state the next scenario
// inherits. What replaces it is more direct and proves more: the handoff
// itself is an authenticated internal call carrying the instant, so the
// harness makes the call late. A handoff whose `firstVisibleAt` is well in the
// past IS a delivery retried through an outage, and it is also what a core
// running ahead of payments would produce. The rule under test is the same
// either way — the window opens at the instant evidence supplied and closes a
// window later — and now it is tested at the seam the rule lives on.

// handoffDelay is how far in the past the delayed handoff's instant is.
//
// Derived from CLOCK_SKEW_ALERT rather than picked: it must be comfortably
// past the threshold, because half of what this scenario asserts is that the
// disagreement was counted rather than absorbed.
var handoffDelay = 5 * harness.SkewAlert

// A claim handed off late must open the window the worker was owed, at the
// instant the record became visible to them — not a fresh one starting when
// the delivery finally landed.
//
// Before #221 this window would have closed handoffDelay late, and nothing
// anywhere would have said so.
func TestADelayedHandoffStillOpensTheWindowWhenTheRecordBecameVisible(t *testing.T) {
	w := setup(t)

	before := w.skewEvents(t)

	// A claim that exists in evidence, so the window being opened is a window
	// over a real record rather than an invention of the test.
	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	claimID := onlyClaim(t, w.submit(t, batch(row(phone, 3, "HH-221A"))))

	var opened winView
	eventually(t, "the window opens on the claim", 20*time.Second, func() error {
		var err error
		opened, err = w.window(claimID)
		return err
	})

	// Now the delayed redelivery of that same handoff, carrying an instant
	// handoffDelay in the past. The payments process must honour the supplied
	// instant, and it must be visibly a redelivery rather than a second window.
	firstVisible := time.Now().UTC().Add(-handoffDelay)
	late := map[string]any{
		"claimId": claimID, "unitId": opened.UnitID, "partyId": opened.PartyID,
		"contextId": fixtures.ProjectID, "definitionId": fixtures.DefinitionID,
		"definitionVersion": 1,
		"createdAt":         firstVisible,
		"firstVisibleAt":    firstVisible,
	}
	var reopened winView
	if err := w.Payments.Post(w.ctx, "/internal/windows", late, &reopened); err != nil {
		t.Fatalf("deliver the late handoff: %v", err)
	}

	// One window, not two. A redelivery that created a second window would be
	// a worker with two deadlines on one claim.
	if reopened.ClaimID != opened.ClaimID {
		t.Fatalf("the redelivery produced a window on %s, not on %s", reopened.ClaimID, opened.ClaimID)
	}

	// And a genuinely late first handoff opens where it was owed. Asserted on
	// a claim of its own, so the redelivery above cannot be what is being read.
	lateClaim := onlyClaim(t, w.submit(t, batch(row(phone, 4, "HH-221C"))))
	var lateWin winView
	eventually(t, "the window opens on the second claim", 20*time.Second, func() error {
		var err error
		lateWin, err = w.window(lateClaim)
		return err
	})
	if got := lateWin.ClosesAt.Sub(lateWin.OpenedAt); got < window-time.Second || got > window+time.Second {
		t.Errorf("the window is %s long; this stack's CONFIRMATION_WINDOW is %s, and the length "+
			"of a worker's chance to object is not something the process that receives the "+
			"handoff gets to decide", got, window)
	}

	// The disagreement was noticed rather than absorbed. A log line nobody
	// greps is not a detector.
	eventually(t, "the clock-skew counter records the late handoff",
		harness.Patience(time.Second), func() error {
			if now := w.skewEvents(t); now <= before {
				return fmt.Errorf("the counter reads %v, unchanged from %v, after a handoff %s "+
					"out of step; a deployment whose two processes drift would have no symptom "+
					"anywhere", now, before, handoffDelay)
			}
			return nil
		})
}

// The unclear-queue path, where the two instants the handoff carries are
// genuinely different: the record entered CREST when the batch arrived, and the
// worker could first have seen it only when somebody put their name to it,
// later. The window runs from the second.
//
// The full-window-from-resolution guarantee has a scenario of its own
// (TestWorkResolvedLateStillGetsItsFullWindow). What this one pins down is
// that the guarantee holds because evidence said when the record became
// visible — not because payments happened to handle the message promptly.
func TestTheUnclearPathOpensTheWindowWhenTheRecordBecameVisibleNotWhenItArrived(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-221B")

	unitArrived, err := w.Parties.Now(w.ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The row sits in the queue. Deliberate input, not a wait for an outcome:
	// the whole point is that the two instants are apart, and past
	// CLOCK_SKEW_ALERT so the difference is visible rather than rounding.
	time.Sleep(3 * harness.SkewAlert)

	code, res := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.CustodianID)
	if code != http.StatusOK {
		t.Fatalf("resolve refused with %d", code)
	}

	var win winView
	eventually(t, "the window opens on the resolved claim", 20*time.Second, func() error {
		var err error
		win, err = w.window(res.ClaimID)
		return err
	})

	// The two instants the handoff carried, seen from the two things they
	// produced: the unit still says the record entered CREST when the batch
	// did, and the window opened when the worker could first have seen it.
	unit := w.unit(t, res.UnitID)
	if unit.CreatedAt.After(unitArrived.Add(2 * harness.SkewAlert)) {
		t.Errorf("the unit says the record entered CREST at %s; it arrived at about %s, and "+
			"stamping it at resolution would make late-resolved work look freshly reported",
			unit.CreatedAt, unitArrived)
	}
	if gap := win.OpenedAt.Sub(unit.CreatedAt); gap < 2*harness.SkewAlert {
		t.Errorf("the window opened %s after the record entered CREST; on this path the two "+
			"instants are apart and the window must run from the later one", gap)
	}
	if drift := win.OpenedAt.Sub(res.ResolvedAt); drift > time.Second || drift < -time.Second {
		t.Errorf("the window opened %s away from the resolution that made the record visible "+
			"(%s vs %s); that difference is the payments process's own clock leaking back in",
			drift, win.OpenedAt, res.ResolvedAt)
	}
}

type unitView struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

func (w *world) unit(t *testing.T, unitID string) unitView {
	t.Helper()
	var out unitView
	if err := w.Evidence.As(w.login(t, fixtures.CustodianID)).
		Get(w.ctx, "/v1/units/"+unitID, &out); err != nil {
		t.Fatalf("read unit %s: %v", unitID, err)
	}
	return out
}

// skewEvents reads the detector the ruling asked for: the counter payments
// publishes on /internal/metrics, beside the outbox gauges.
func (w *world) skewEvents(t *testing.T) float64 {
	t.Helper()
	code, raw, err := w.Payments.StatusRaw(w.ctx, http.MethodGet, "/internal/metrics", "", nil)
	if err != nil {
		t.Fatalf("read the payments metrics: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("the payments metrics answered %d: %s", code, raw)
	}
	value, ok := prometheusValue(string(raw), "crest_window_clock_skew_events")
	if !ok {
		t.Fatalf("no crest_window_clock_skew_events in the payments metrics:\n%s", raw)
	}
	return value
}

// prometheusValue reads one sample out of the exposition. Deliberately tiny:
// pulling in a Prometheus parser to read a single counter would be a dependency
// nobody could justify at review.
func prometheusValue(body, name string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}
