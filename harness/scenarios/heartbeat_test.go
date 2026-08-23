//go:build e2e

package scenarios

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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

func (w *world) registerSource(t *testing.T, every string) sourceView {
	t.Helper()
	var out sourceView
	if err := w.Evidence.Post(w.ctx, "/v1/sources", map[string]any{
		"adapterRef":    "dhis2-riverside-" + runID,
		"contextId":     fixtures.ProjectID,
		"systemRef":     "dhis2-riverside",
		"expectedEvery": every,
		"ownerPartyId":  fixtures.SupervisorID,
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
		code, resp, err := w.Evidence.Status(w.ctx, http.MethodPost, "/v1/sources", body)
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
	src := w.registerSource(t, "24h")

	// A batch is a heartbeat. Submitted through the same endpoint a real
	// source uses, with the adapter this source is registered under.
	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=%s",
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID, "dhis2-riverside-"+runID)
	var ingested ingestResult
	if err := w.Evidence.PostRaw(w.ctx, path, "text/csv",
		batch(row(phone, 3, "HH-HB-"+runID)), &ingested); err != nil {
		t.Fatalf("submit batch: %v", err)
	}

	var list sourceList
	if err := w.Evidence.Get(w.ctx, "/v1/sources", &list); err != nil {
		t.Fatal(err)
	}
	if got := find(list, src.ID); got.State != "HEALTHY" {
		t.Fatalf("a source that just sent a batch is %s", got.State)
	}

	// Nothing arrives for more than a day. The clock moves rather than the
	// test waiting — the whole point of a driveable clock.
	if err := w.Advance(w.ctx, 25*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := w.Evidence.Get(w.ctx, "/v1/sources", &list); err != nil {
		t.Fatal(err)
	}
	got := find(list, src.ID)
	if got.State != "SILENT" {
		t.Fatalf("25 hours after its last batch, a daily source is %s", got.State)
	}
	if list.Silent < 1 {
		t.Error("the count a monitor alerts on does not include the silent source")
	}

	var swept sweepResult
	if err := w.Evidence.Post(w.ctx, "/v1/sources/sweep", nil, &swept); err != nil {
		t.Fatal(err)
	}
	if !contains(swept.WentQuiet, src.ID) {
		t.Fatalf("the sweep did not discover the silent source: %+v", swept)
	}

	// The owner is told, and the message is addressed to somebody who has to go
	// and ask a question rather than somebody who already knows the system.
	var messages []struct {
		To   string `json:"to"`
		Body string `json:"body"`
	}
	supervisorPhone, err := harness.PhoneOf(w.w, fixtures.SupervisorID)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the source's owner is told", 20*time.Second, func() error {
		// Escaped: a phone number starts with '+', which in a query string
		// means a space, so the unescaped form looks up a different number and
		// finds nothing.
		if err := w.SMS.Get(w.ctx, "/messages?to="+url.QueryEscape(supervisorPhone), &messages); err != nil {
			return err
		}
		for _, m := range messages {
			if strings.Contains(m.Body, "stopped sending") {
				return nil
			}
		}
		return fmt.Errorf("no outage message among %d", len(messages))
	})

	// Swept again with nothing fixed: still quiet, and NOT reported as a new
	// discovery. An alert per sweep is how an alert channel becomes something
	// people mute, and the muted channel is the one the real outage arrives on.
	var again sweepResult
	if err := w.Evidence.Post(w.ctx, "/v1/sources/sweep", nil, &again); err != nil {
		t.Fatal(err)
	}
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
	src := w.registerSource(t, "24h")

	if err := w.Advance(w.ctx, 25*time.Hour); err != nil {
		t.Fatal(err)
	}
	var swept sweepResult
	if err := w.Evidence.Post(w.ctx, "/v1/sources/sweep", nil, &swept); err != nil {
		t.Fatal(err)
	}

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=%s",
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID, "dhis2-riverside-"+runID)
	var ingested ingestResult
	if err := w.Evidence.PostRaw(w.ctx, path, "text/csv",
		batch(row(phone, 2, "HH-RESUME-"+runID)), &ingested); err != nil {
		t.Fatalf("submit batch: %v", err)
	}

	var list sourceList
	if err := w.Evidence.Get(w.ctx, "/v1/sources", &list); err != nil {
		t.Fatal(err)
	}
	if got := find(list, src.ID); got.State != "HEALTHY" {
		t.Fatalf("a source that resumed sending is still %s", got.State)
	}

	// The episode is closed, so a second outage later is discovered again
	// rather than suppressed by the first one's record.
	if err := w.Advance(w.ctx, 25*time.Hour); err != nil {
		t.Fatal(err)
	}
	var second sweepResult
	if err := w.Evidence.Post(w.ctx, "/v1/sources/sweep", nil, &second); err != nil {
		t.Fatal(err)
	}
	if !contains(second.WentQuiet, src.ID) {
		t.Fatalf("a second outage was not discovered; the first episode was never closed: %+v", second)
	}
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
