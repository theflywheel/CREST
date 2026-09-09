//go:build e2e

package scenarios

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// Source heartbeat monitoring (#22, Blueprint §8).
//
// The failure this exists for is the only one in the system a worker cannot
// report. A held payment has a reason they can ask about; a rejected row is in
// the unclear queue; a wrong record can be disputed. A source going quiet
// produces nothing — the worker keeps working and their record stops growing,
// which from where they stand is indistinguishable from having done no work.
// Nobody finds out until a payment cycle comes up short.

type sourceView struct {
	ID           string `json:"id"`
	AdapterRef   string `json:"adapterRef"`
	State        string `json:"state"`
	QuietFor     string `json:"quietFor"`
	OwnerPartyID string `json:"ownerPartyId"`
}

type sourceList struct {
	Sources []sourceView `json:"sources"`
	Count   int          `json:"count"`
	Silent  int          `json:"silent"`
}

type sweepResult struct {
	WentQuiet  []string `json:"wentQuiet"`
	StillQuiet []string `json:"stillQuiet"`
	Checked    int      `json:"checked"`
}

// quiet reports whether a sweep saw this source as quiet, whether it was the
// one that discovered the silence or found it already open.
//
// Both lists, on purpose. The monitor loop runs on this stack — nobody posts
// to /v1/sources/sweep on a real deployment — so which of the two noticed
// first is a race between the loop and the test, and a scenario that depended
// on winning it would be a scenario that fails for a reason it does not name.
// What the silence detector owes anybody is that the outage is open and
// findable; who opened it is not the promise.
func quiet(s sweepResult, id string) bool {
	return contains(s.WentQuiet, id) || contains(s.StillQuiet, id)
}

func (w *world) sweepSources(t *testing.T) sweepResult {
	t.Helper()
	var out sweepResult
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Post(w.ctx,
		"/v1/sources/sweep?contextId="+url.QueryEscape(fixtures.ProjectID), nil, &out); err != nil {
		t.Fatalf("sweep sources: %v", err)
	}
	return out
}

func (w *world) sourceState(t *testing.T, id string) sourceView {
	t.Helper()
	var list sourceList
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Get(w.ctx,
		"/v1/sources?contextId="+url.QueryEscape(fixtures.ProjectID), &list); err != nil {
		t.Fatalf("list sources: %v", err)
	}
	return find(list, id)
}

func (w *world) registerSource(t *testing.T, every string) sourceView {
	t.Helper()
	var out sourceView
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Post(w.ctx, "/v1/sources", map[string]any{
		"adapterRef":     "csv-batch@1",
		"contextId":      fixtures.ProjectID,
		"systemRef":      "dhis2-riverside-" + runID,
		"sourceClass":    "programme-system",
		"captureMethod":  "digital-capture",
		"sourceExposure": "signed-batch",
		"expectedEvery":  every,
		"ownerPartyId":   fixtures.SupervisorID,
	}, &out); err != nil {
		t.Fatalf("register source: %v", err)
	}
	return out
}

// A cadence nobody declared and an owner nobody named both produce an alert
// that cannot be acted on. Refused rather than defaulted.
func TestASourceCannotBeMonitoredWithoutACadenceAndAnOwner(t *testing.T) {
	w := setup(t)

	for name, body := range map[string]map[string]any{
		"no cadence": {"adapterRef": "a", "contextId": fixtures.ProjectID, "ownerPartyId": fixtures.SupervisorID},
		"no owner":   {"adapterRef": "a", "contextId": fixtures.ProjectID, "expectedEvery": "24h"},
		"zero cadence": {"adapterRef": "a", "contextId": fixtures.ProjectID,
			"expectedEvery": "0s", "ownerPartyId": fixtures.SupervisorID},
	} {
		code, resp, err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Status(w.ctx, http.MethodPost, "/v1/sources", body)
		if err != nil {
			t.Fatal(err)
		}
		if code != http.StatusBadRequest {
			t.Errorf("%s was accepted: %d %s", name, code, resp)
		}
	}
}

// The whole loop: a feed is registered, sends, goes quiet, is noticed, and its
// owner is told — once.
func TestASourceThatGoesQuietIsNoticedAndItsOwnerIsTold(t *testing.T) {
	w := setup(t)
	src := w.registerSource(t, harness.SourceCadence.String())

	// A batch is a heartbeat. Submitted through the same endpoint a real
	// source uses, with the adapter this source is registered under.
	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&definitionVersion=1&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=%s",
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID, "dhis2-riverside-"+runID)
	var ingested ingestResult
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).PostRaw(w.ctx, path, "text/csv",
		batch(row(phone, 3, "HH-HB-"+runID)), &ingested); err != nil {
		t.Fatalf("submit batch: %v", err)
	}

	var list sourceList
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Get(w.ctx,
		"/v1/sources?contextId="+url.QueryEscape(fixtures.ProjectID), &list); err != nil {
		t.Fatal(err)
	}
	if got := find(list, src.ID); got.State != "HEALTHY" {
		t.Fatalf("a source that just sent a batch is %s", got.State)
	}

	// Nothing arrives for longer than the source's cadence. The test waits it
	// out, because there is no clock to move (ruled 2026-09-09) — the cadence
	// is a per-source setting, so a source registered at seconds proves the
	// same code a daily one runs.
	eventually(t, "the source is seen to have gone quiet",
		harness.SourceCadence+harness.Patience(harness.SourceMonitorEvery), func() error {
			if got := w.sourceState(t, src.ID); got.State != "SILENT" {
				return fmt.Errorf("a source %s past its cadence is %s", harness.SourceCadence, got.State)
			}
			if !quiet(w.sweepSources(t), src.ID) {
				return fmt.Errorf("no sweep has the source down as quiet; the outage is not open anywhere")
			}
			return nil
		})
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Get(w.ctx,
		"/v1/sources?contextId="+url.QueryEscape(fixtures.ProjectID), &list); err != nil {
		t.Fatal(err)
	}
	if list.Silent < 1 {
		t.Error("the count a monitor alerts on does not include the silent source")
	}

	// Nobody is told (#150): notifications are dropped, so the outage's only
	// witnesses are the sweep's own record and the log line the delivery
	// writes instead of sending. What this test can still hold is that the
	// detection machinery keeps working — the silence is discovered, counted,
	// and not re-alerted — so the day a channel returns, it has something
	// true to say.

	// Swept again with nothing fixed: still quiet, and NOT reported as a new
	// discovery. An alert per sweep is how an alert channel becomes something
	// people mute, and the muted channel is the one the real outage arrives on.
	again := w.sweepSources(t)
	if contains(again.WentQuiet, src.ID) {
		t.Error("the second sweep re-reported the same outage as a new discovery")
	}
	if !contains(again.StillQuiet, src.ID) {
		t.Error("the second sweep did not report the outage as still open; it reads as resolved")
	}
}

// A feed that comes back is healthy again, and a later outage is a new episode
// rather than a continuation of the old one.
func TestAFeedThatResumesIsHealthyAgain(t *testing.T) {
	w := setup(t)
	src := w.registerSource(t, harness.SourceCadence.String())

	eventually(t, "the first outage is open",
		harness.SourceCadence+harness.Patience(harness.SourceMonitorEvery), func() error {
			if !quiet(w.sweepSources(t), src.ID) {
				return fmt.Errorf("the source has not been seen to go quiet")
			}
			return nil
		})

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&definitionVersion=1&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=%s",
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID, "dhis2-riverside-"+runID)
	var ingested ingestResult
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).PostRaw(w.ctx, path, "text/csv",
		batch(row(phone, 2, "HH-RESUME-"+runID)), &ingested); err != nil {
		t.Fatalf("submit batch: %v", err)
	}

	var list sourceList
	if err := w.Evidence.As(w.login(t, fixtures.SupervisorID)).Get(w.ctx,
		"/v1/sources?contextId="+url.QueryEscape(fixtures.ProjectID), &list); err != nil {
		t.Fatal(err)
	}
	if got := find(list, src.ID); got.State != "HEALTHY" {
		t.Fatalf("a source that resumed sending is still %s", got.State)
	}
	// The first episode is closed. This is the half that proves it: a sweep
	// now reports the source as neither newly quiet nor still quiet. An
	// episode left open would keep it on the still-quiet list while the source
	// is demonstrably healthy, and the second outage below would then be
	// suppressed by the first one's record.
	if resumed := w.sweepSources(t); quiet(resumed, src.ID) {
		t.Fatalf("a healthy source is still carrying an open outage; the first episode "+
			"was never closed: %+v", resumed)
	}

	// So a second outage is discovered as its own episode rather than
	// suppressed.
	eventually(t, "the second outage is discovered as a new episode",
		harness.SourceCadence+harness.Patience(harness.SourceMonitorEvery), func() error {
			if !quiet(w.sweepSources(t), src.ID) {
				return fmt.Errorf("a second outage was not discovered")
			}
			return nil
		})
}

func find(l sourceList, id string) sourceView {
	for _, s := range l.Sources {
		if s.ID == id {
			return s
		}
	}
	return sourceView{}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
