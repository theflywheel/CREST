//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// Working the unclear queue (#25).
//
// The queue could be listed from the day it existed and never worked. Every
// row in it is work somebody did — the record is sound, and the only missing
// fact is who. Left unresolvable, the queue is a list of people who did the
// work and are not going to be paid, growing.
//
// What these scenarios pin down is not that resolution happens, but the three
// decisions inside it: that a late-resolved row gets its full seven days, that
// only a row whose *record* was sound can be attached to a person, and that
// whoever sends the file is not the one who decides whose work it was.

type resolution struct {
	UnclearRowID string    `json:"unclearRowId"`
	PartyID      string    `json:"partyId"`
	ResolvedBy   string    `json:"resolvedBy"`
	ResolvedAt   time.Time `json:"resolvedAt"`
	UnitID       string    `json:"unitId"`
	ClaimID      string    `json:"claimId"`
	Note         string    `json:"note"`
}

// unattributedRow submits a batch carrying one row nobody can be matched to,
// and returns the queue row it produced.
func (w *world) unattributedRow(t *testing.T, householdID string) string {
	t.Helper()
	result := w.submit(t, batch(row("+15550199999", 7, householdID)))
	if len(result.Unclear) != 1 {
		t.Fatalf("expected one unclear row, got %d", len(result.Unclear))
	}
	if got := result.Unclear[0].Kind; got != "unattributed" {
		t.Fatalf("the row is %q, want unattributed: %s", got, result.Unclear[0].Reason)
	}
	if result.Unclear[0].ID == "" {
		t.Fatal("the unclear row has no id, so nothing can address it")
	}
	return result.Unclear[0].ID
}

func (w *world) resolveUnclear(t *testing.T, rowID, partyID, by string) (int, resolution) {
	t.Helper()
	var out resolution
	code, body, err := w.Evidence.Status(w.ctx, http.MethodPost,
		"/v1/unclear/"+rowID+"/resolve",
		map[string]any{"partyId": partyID, "resolvedByPartyId": by})
	if err != nil {
		t.Fatalf("resolve %s: %v", rowID, err)
	}
	if code == http.StatusOK {
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("read the resolution: %v (%s)", err, body)
		}
	}
	return code, out
}

// The whole point: a row nobody could attribute becomes that worker's claim,
// and the worker gets the same seven days to object that everyone else gets.
func TestAnUnattributedRowBecomesTheClaimOfTheWorkerWhoDidTheWork(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-901")

	code, res := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.CustodianID)
	if code != http.StatusOK {
		t.Fatalf("resolve refused with %d", code)
	}
	if res.ClaimID == "" {
		t.Fatal("the row was closed without producing a claim: the work is now in neither place")
	}

	var win winView
	eventually(t, "the confirmation window opens on the resolved claim", 15*time.Second, func() error {
		var err error
		win, err = w.window(res.ClaimID)
		return err
	})
	if win.PartyID != fixtures.WorkerCID {
		t.Errorf("the window belongs to %s, want the worker it was resolved to", win.PartyID)
	}

	// Gone from the queue, and it names who resolved it. A resolution with no
	// named resolver is the same shape of unaccountable as a held payment with
	// no owner.
	if res.ResolvedBy != fixtures.CustodianID {
		t.Errorf("resolved by %q, want the custodian", res.ResolvedBy)
	}
	for _, open := range w.openQueue(t) {
		if open == rowID {
			t.Error("the row is still in the queue after being resolved")
		}
	}
}

// The decision this scenario exists for: the clock starts when the worker could
// first have seen the record, not when the file arrived. A row resolved three
// weeks late would otherwise exit its window the instant it was created —
// auto-confirmed without the worker ever being asked.
func TestWorkResolvedWeeksLateStillGetsItsFullSevenDays(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-902")

	if err := w.Advance(w.ctx, 20*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	code, res := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.CustodianID)
	if code != http.StatusOK {
		t.Fatalf("resolve refused with %d", code)
	}

	var win winView
	eventually(t, "the window opens", 15*time.Second, func() error {
		var err error
		win, err = w.window(res.ClaimID)
		return err
	})

	remaining := win.ClosesAt.Sub(res.ResolvedAt)
	if remaining < 6*24*time.Hour {
		t.Errorf("the window closes %s after resolution; a worker shown a record for the first "+
			"time gets seven days to object to it, not %s", remaining, remaining)
	}
	if win.ExitRoute != nil {
		t.Errorf("the window had already exited via %q at the moment it opened", *win.ExitRoute)
	}
}

// A row that failed the evidence contract is not re-attributable, and the
// refusal says which kind it is. Attaching a person to it would launder a
// record that failed the contract into the ledger wearing a worker's name.
func TestARowThatFailedTheContractCannotBeGivenToAWorker(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	// Right shape, wrong activity: the definition does not count this.
	bad := strings.Replace(row(phone, 4, "HH-903"), "bednet-distribution", "malaria-screening", 1)
	result := w.submit(t, batch(bad))
	if len(result.Unclear) != 1 || result.Unclear[0].Kind != "contract" {
		t.Fatalf("expected one contract-failed row, got %+v", result.Unclear)
	}

	code, _ := w.resolveUnclear(t, result.Unclear[0].ID, fixtures.WorkerAID, fixtures.CustodianID)
	if code != http.StatusConflict {
		t.Errorf("a contract-failed row was resolvable: %d", code)
	}
}

// Whoever submits the file must not also decide whose work its unmatched rows
// were. The supervisor may submit evidence and holds no grant to resolve, and
// the two must not be the same permission.
func TestSubmittingEvidenceDoesNotLetYouDecideWhoseWorkItWas(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-904")

	code, _ := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.SupervisorID)
	if code != http.StatusForbidden {
		t.Errorf("the submitter resolved a row they hold no grant for: %d", code)
	}

	// And the row is untouched, so the custodian can still work it.
	code, res := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.CustodianID)
	if code != http.StatusOK || res.ClaimID == "" {
		t.Errorf("the refused attempt consumed the row: %d %+v", code, res)
	}
}

// Twice is the case that pays somebody twice, so it is refused rather than
// deduplicated after the fact.
func TestARowCannotBeResolvedTwice(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-905")

	if code, _ := w.resolveUnclear(t, rowID, fixtures.WorkerCID, fixtures.CustodianID); code != http.StatusOK {
		t.Fatalf("first resolve refused with %d", code)
	}
	code, _ := w.resolveUnclear(t, rowID, fixtures.WorkerBID, fixtures.CustodianID)
	if code != http.StatusNotFound {
		t.Errorf("the same work was resolved to a second worker: %d", code)
	}
}

// A worker who has withdrawn enrolment consent has asked us to stop recording
// evidence about them (§9). Resolving a row onto them would record it anyway,
// through a door ingestion has closed.
func TestARowCannotBeResolvedOntoAWorkerWhoWithdrewConsent(t *testing.T) {
	w := setup(t)
	rowID := w.unattributedRow(t, "HH-906")

	// A worker created for this scenario rather than a fixture one. Withdrawal
	// is permanent and the scenarios share a stack, so withdrawing on behalf of
	// Worker C would silently break every later test that expects Worker C to
	// be payable — which is exactly what it did the first time this ran.
	party := newWorkerWithBinding(t, w, "Withdrawn Worker "+runID, "")
	w.withdrawEnrolmentConsent(t, party)

	code, _ := w.resolveUnclear(t, rowID, party, fixtures.CustodianID)
	if code != http.StatusConflict {
		t.Errorf("evidence was recorded about a worker who withdrew consent: %d", code)
	}
}

func (w *world) openQueue(t *testing.T) []string {
	t.Helper()
	var queue struct {
		Unclear []struct {
			ID string `json:"id"`
		} `json:"unclear"`
	}
	if err := w.Evidence.Get(w.ctx, "/v1/unclear", &queue); err != nil {
		t.Fatalf("list the unclear queue: %v", err)
	}
	ids := make([]string, 0, len(queue.Unclear))
	for _, u := range queue.Unclear {
		ids = append(ids, u.ID)
	}
	return ids
}

// withdrawEnrolmentConsent records an enrolment consent for a worker on this
// project and then withdraws it, which is the only way to reach the WITHDRAWN
// state through the same doors a deployment uses.
func (w *world) withdrawEnrolmentConsent(t *testing.T, partyID string) {
	t.Helper()
	path := fmt.Sprintf("/v1/parties/%s/consents?moment=enrolment&captureMethod=screen"+
		"&purpose=%s&capturedBy=%s&contextId=%s",
		partyID, url.QueryEscape("hold and fetch evidence of my work"),
		url.QueryEscape(fixtures.SupervisorID), url.QueryEscape(fixtures.ProjectID))

	var consent struct {
		ID string `json:"id"`
	}
	if err := w.Registry.Post(w.ctx, path, nil, &consent); err != nil {
		t.Fatalf("record consent: %v", err)
	}
	if err := w.Registry.Post(w.ctx, "/v1/consents/"+consent.ID+"/withdraw",
		map[string]any{"reason": "asked to be removed from the programme"}, nil); err != nil {
		t.Fatalf("withdraw consent: %v", err)
	}
}
