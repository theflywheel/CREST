//go:build e2e

package scenarios

import (
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
// two processes' clocks. Both are what these scenarios reproduce — by driving
// the payments clock away from core's, which is exactly the disagreement a
// deployment would suffer and the harness previously could not create, because
// it only ever drove the two together.
//
// Every one of these leaves the stack's clocks aligned again. A scenario that
// hands the next one a skewed stack is a scenario that fails somebody else's
// test.

const handoffDelay = 72 * time.Hour

// The delivery lands three days after the claim was created — an outbox retry
// through an outage, reproduced by putting the payments process three days
// ahead of core before the batch is submitted. The worker is owed seven days
// from the moment they could first have seen the record, so the window closes
// where it always would have and simply has less of itself left.
//
// Before #221 this window would have closed three days late, and nothing
// anywhere would have said so.
func TestADelayedHandoffStillClosesSevenDaysAfterTheRecordBecameVisible(t *testing.T) {
	w := setup(t)

	// Payments alone, forward. Restored below, in both the passing and the
	// failing case, by walking core the same distance.
	if err := w.Payments.Post(w.ctx, "/internal/clock",
		map[string]any{"advance": handoffDelay.String()}, nil); err != nil {
		t.Fatalf("put the payments clock ahead: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Parties.Post(w.ctx, "/internal/clock",
			map[string]any{"advance": handoffDelay.String()}, nil); err != nil {
			t.Fatalf("realign the core clock; the next scenario would run on a skewed stack: %v", err)
		}
	})

	coreNow, err := w.Parties.Now(w.ctx)
	if err != nil {
		t.Fatal(err)
	}
	paymentsNow, err := w.Payments.Now(w.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if paymentsNow.Sub(coreNow) < handoffDelay/2 {
		t.Fatalf("the two clocks are %s apart; this scenario has nothing to prove without the skew "+
			"it just created", paymentsNow.Sub(coreNow))
	}

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	claimID := onlyClaim(t, w.submit(t, batch(row(phone, 3, "HH-221A"))))

	var win winView
	eventually(t, "the window opens on the delayed handoff", 20*time.Second, func() error {
		var err error
		win, err = w.window(claimID)
		return err
	})

	// The claim was created on core's clock, so that is where the seven days
	// run from. Anything near the payments clock is the pre-#221 behaviour.
	window := win.ClosesAt.Sub(win.OpenedAt)
	if win.OpenedAt.After(coreNow.Add(time.Hour)) {
		t.Errorf("the window opened at %s, which is the process that received the handoff reading "+
			"its own clock; the worker could first have seen this record at about %s",
			win.OpenedAt, coreNow)
	}
	if win.ClosesAt.After(coreNow.Add(window).Add(time.Hour)) {
		t.Errorf("the window closes at %s — %s after the worker could first have seen the record. "+
			"A delivery held up by a retry must not hand out a fresh window; it must open the one "+
			"that was owed, late", win.ClosesAt, win.ClosesAt.Sub(coreNow))
	}

	// And the disagreement was noticed rather than absorbed. A log line nobody
	// greps is not a detector.
	if events := w.skewEvents(t); events < 1 {
		t.Errorf("the clock-skew counter reads %v after a handoff three days out of step; "+
			"a deployment whose two processes drift would have no symptom anywhere", events)
	}
}

// The unclear-queue path, where the two instants the handoff carries are
// genuinely different: the record entered CREST when the batch arrived, and the
// worker could first have seen it only when somebody put their name to it,
// twenty days later. The window runs from the second.
//
// The seven-days-from-resolution guarantee has a scenario of its own
// (TestWorkResolvedWeeksLateStillGetsItsFullSevenDays). What this one pins down
// is that the guarantee now holds because evidence said when the record became
// visible — not because payments happened to handle the message promptly.
func TestTheUnclearPathOpensTheWindowWhenTheRecordBecameVisibleNotWhenItArrived(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-221B")

	unitArrived, err := w.Parties.Now(w.ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Advance(w.ctx, 20*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	// And the handoff for the resolved claim is delivered late, on top: without
	// this the two clocks move together and the old behaviour — payments
	// opening the window on its own clock — would look identical to the new
	// one. Realigned in the cleanup, whichever way this scenario ends.
	if err := w.Payments.Post(w.ctx, "/internal/clock",
		map[string]any{"advance": handoffDelay.String()}, nil); err != nil {
		t.Fatalf("put the payments clock ahead: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Parties.Post(w.ctx, "/internal/clock",
			map[string]any{"advance": handoffDelay.String()}, nil); err != nil {
			t.Fatalf("realign the core clock; the next scenario would run on a skewed stack: %v", err)
		}
	})

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
	// produced: the unit still says the record entered CREST twenty days ago,
	// and the window opened when the worker could first have seen it.
	unit := w.unit(t, res.UnitID)
	if unit.CreatedAt.After(unitArrived.Add(time.Hour)) {
		t.Errorf("the unit says the record entered CREST at %s; it arrived at about %s, and "+
			"stamping it at resolution would make late-resolved work look freshly reported",
			unit.CreatedAt, unitArrived)
	}
	if gap := win.OpenedAt.Sub(unit.CreatedAt); gap < 19*24*time.Hour {
		t.Errorf("the window opened %s after the record entered CREST; on this path the two "+
			"instants are twenty days apart and the window must run from the later one", gap)
	}
	if drift := win.OpenedAt.Sub(res.ResolvedAt); drift > time.Hour || drift < -time.Hour {
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
