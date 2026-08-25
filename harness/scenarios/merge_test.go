//go:build e2e

package scenarios

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// A worker's history is continuous across a merge (#100).
//
// #99 made a merge expressible: the custodian decides, the worker confirms, and
// the absorbed party is marked `mergedInto`. What it did not do is make anybody
// else notice. Claims, confirmation windows and payment instructions written
// before the merge all name the absorbed party, and nothing mapped them — so a
// worker whose duplicate was closed found a hole in their record exactly where
// the system had corrected its own mistake about who they were.
//
// The fix is on the read and not on the write, deliberately. Rewriting those
// rows to name the survivor would destroy the only record of what was believed
// at the time, and a claim that said whose work it was then is a true
// statement. So the records stay where they are and every read follows the
// pointer.

type claimList struct {
	Claims []struct {
		ID      string `json:"id"`
		PartyID string `json:"partyId"`
	} `json:"claims"`
}

type windowList struct {
	Windows []struct {
		ClaimID string `json:"claimId"`
		PartyID string `json:"partyId"`
	} `json:"windows"`
	Count int `json:"count"`
}

type instructionList struct {
	Instructions []struct {
		ID      string `json:"id"`
		PartyID string `json:"partyId"`
	} `json:"instructions"`
}

// rosterWork puts one row of work on the books against a roster id, so a party
// can be given work without a phone number — which matters here because the two
// parties in a duplicate hold share a phone by construction, and matching on it
// is exactly what the registry refuses to do.
func (w *world) rosterWork(t *testing.T, party, rosterID, household string) string {
	t.Helper()
	if err := w.Registry.Post(w.ctx, "/v1/parties/"+url.PathEscape(party)+"/roster-ids",
		map[string]any{"rosterId": rosterID, "contextId": fixtures.ProjectID}, nil); err != nil {
		t.Fatalf("register roster id %s: %v", rosterID, err)
	}
	csv := []byte("activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
		"period_start,household_id,source_record_ref\n" +
		fmt.Sprintf("bednet-distribution,3,bednets-distributed,roster-id,%s,2026-03-02,%s,%s-%s\n",
			rosterID, household, runID, rosterID))

	res := w.submit(t, csv)
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("the roster id did not match %s: %+v", party, res.Unclear)
	}
	return res.ClaimIDs[0]
}

func (w *world) claimsOf(t *testing.T, party string) claimList {
	t.Helper()
	var out claimList
	if err := w.Evidence.Get(w.ctx, "/v1/claims?partyId="+url.QueryEscape(party), &out); err != nil {
		t.Fatalf("list claims for %s: %v", party, err)
	}
	return out
}

func (w *world) windowsOf(t *testing.T, party string) windowList {
	t.Helper()
	var out windowList
	if err := w.Confirmation.Get(w.ctx, "/v1/windows?partyId="+url.QueryEscape(party), &out); err != nil {
		t.Fatalf("list windows for %s: %v", party, err)
	}
	return out
}

func (w *world) instructionsOf(t *testing.T, party string) instructionList {
	t.Helper()
	var out instructionList
	if err := w.Payments.Get(w.ctx, "/v1/instructions?partyId="+url.QueryEscape(party), &out); err != nil {
		t.Fatalf("list instructions for %s: %v", party, err)
	}
	return out
}

func has(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestAWorkersHistoryIsContinuousAcrossAMerge(t *testing.T) {
	w := setup(t)

	// One person recorded twice, each record with work of its own. This is the
	// ordinary way a duplicate happens: somebody enrolled at a field visit and
	// again at a clinic, and both records collected real work before anybody
	// noticed they were the same person.
	survivor, absorbed, hold := w.twoPartiesOneNumber(t, sharedNumber(301))
	claimA := w.rosterWork(t, survivor, "MRG-A-"+runID[:6], "HH-merge-a")
	claimB := w.rosterWork(t, absorbed, "MRG-B-"+runID[:6], "HH-merge-b")

	// Both windows are confirmed and paid before the merge, so there is a
	// complete record on each side rather than only a claim. A merge that
	// reunites the claims and loses the payments has not reunited anything.
	for _, claimID := range []string{claimA, claimB} {
		eventually(t, "the window opens for "+claimID, 15*time.Second, func() error {
			_, err := w.window(claimID)
			return err
		})
		if err := w.confirmClaim(t, claimID, nil, nil); err != nil {
			t.Fatalf("confirm %s: %v", claimID, err)
		}
	}
	for _, claimID := range []string{claimA, claimB} {
		eventually(t, "the payment for "+claimID+" is released", 20*time.Second, func() error {
			_, err := w.instruction(claimID)
			return err
		})
	}

	// Before the merge the two records are genuinely separate, and asserting it
	// is what stops this scenario passing for the wrong reason — a read that
	// returned everything regardless would satisfy every assertion below.
	if before := w.claimsOf(t, survivor); len(before.Claims) != 1 {
		t.Fatalf("before the merge the survivor has %d claims, want 1", len(before.Claims))
	}

	// The custodian decides and the worker confirms. Neither alone is a merge.
	code, res := w.resolveHold(t, hold.ID, map[string]any{
		"decision":           "merge",
		"partyId":            survivor,
		"resolvedByPartyId":  fixtures.CustodianID,
		"confirmedByPartyId": survivor,
		"confirmationMethod": "in-person",
	})
	if code != 200 {
		t.Fatalf("the merge was refused with %d", code)
	}
	if !has(res.Merged, absorbed) {
		t.Fatalf("the resolution does not say %s was absorbed: %+v", absorbed, res)
	}

	// Evidence: both claims, asked about the survivor.
	claims := w.claimsOf(t, survivor)
	if len(claims.Claims) != 2 {
		t.Fatalf("after the merge the survivor has %d claims, want 2: %+v", len(claims.Claims), claims)
	}
	// And the claims still say who they were recorded against. The merge
	// corrected who this person is; it did not make the earlier record false,
	// and rewriting it would remove the only evidence of what was believed
	// before the correction.
	var sawAbsorbed bool
	for _, c := range claims.Claims {
		if c.PartyID == absorbed {
			sawAbsorbed = true
		}
	}
	if !sawAbsorbed {
		t.Fatalf("no claim still names the absorbed party; history was rewritten: %+v", claims)
	}

	// Confirmation: both windows, with the credentials they issued.
	windows := w.windowsOf(t, survivor)
	if windows.Count != 2 {
		t.Fatalf("after the merge the survivor has %d windows, want 2: %+v", windows.Count, windows)
	}

	// Payments: both instructions. A worker whose duplicate was closed must not
	// find their payments split across two records, neither of which is
	// complete — that is the state somebody disputes at a pay point.
	instructions := w.instructionsOf(t, survivor)
	if len(instructions.Instructions) != 2 {
		t.Fatalf("after the merge the survivor has %d instructions, want 2: %+v",
			len(instructions.Instructions), instructions)
	}
}

func TestAskingAboutTheAbsorbedRecordGivesTheWholeHistory(t *testing.T) {
	w := setup(t)

	// The stale bookmark. A supervisor's device, a printed card, a link in a
	// message from last month — all of them name the id somebody had before the
	// merge, and all of them have to keep working. Answering "that record is
	// gone" would be the system punishing a worker for its own correction.
	survivor, absorbed, hold := w.twoPartiesOneNumber(t, sharedNumber(302))
	w.rosterWork(t, survivor, "OLD-A-"+runID[:6], "HH-old-a")
	w.rosterWork(t, absorbed, "OLD-B-"+runID[:6], "HH-old-b")

	if code, _ := w.resolveHold(t, hold.ID, map[string]any{
		"decision":           "merge",
		"partyId":            survivor,
		"resolvedByPartyId":  fixtures.CustodianID,
		"confirmedByPartyId": survivor,
		"confirmationMethod": "in-person",
	}); code != 200 {
		t.Fatalf("the merge was refused with %d", code)
	}

	if claims := w.claimsOf(t, absorbed); len(claims.Claims) != 2 {
		t.Fatalf("asking about the absorbed id gave %d claims, want the whole history of 2: %+v",
			len(claims.Claims), claims)
	}

	// And the registry says plainly which ids are one person, so a client that
	// wants to display it does not have to infer it from a claim list.
	var ids struct {
		PartyID     string   `json:"partyId"`
		Identifiers []string `json:"identifiers"`
		Merged      bool     `json:"merged"`
	}
	if err := w.Registry.Get(w.ctx,
		"/v1/parties/"+url.PathEscape(absorbed)+"/identifiers", &ids); err != nil {
		t.Fatalf("read identifiers: %v", err)
	}
	if ids.PartyID != survivor {
		t.Fatalf("asked about the absorbed id and got %q as the party, want the survivor", ids.PartyID)
	}
	if !ids.Merged || !has(ids.Identifiers, absorbed) || !has(ids.Identifiers, survivor) {
		t.Fatalf("the identifier list does not describe the merge: %+v", ids)
	}
}

func TestAPartyThatWasNeverMergedReadsTheSameAsBefore(t *testing.T) {
	w := setup(t)

	// The other half of the change, and the one that would break quietly:
	// every list read now asks the registry which ids are this person, and a
	// worker with no merge must get exactly what they got before.
	phone := sharedNumber(303)
	worker := newWorkerWithPhone(t, w, "Never Merged", phone)
	res := w.submit(t, batch(row(phone, 4, "HH-unmerged-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %+v", res)
	}

	if claims := w.claimsOf(t, worker); len(claims.Claims) != 1 {
		t.Fatalf("an unmerged worker has %d claims, want 1: %+v", len(claims.Claims), claims)
	}
	var ids struct {
		Identifiers []string `json:"identifiers"`
		Merged      bool     `json:"merged"`
	}
	if err := w.Registry.Get(w.ctx,
		"/v1/parties/"+url.PathEscape(worker)+"/identifiers", &ids); err != nil {
		t.Fatalf("read identifiers: %v", err)
	}
	if ids.Merged || len(ids.Identifiers) != 1 {
		t.Fatalf("an unmerged worker is described as merged: %+v", ids)
	}
}

func TestAnIncompleteHistoryIsRefusedRatherThanServed(t *testing.T) {
	w := setup(t)

	// Every list read now asks the registry which ids are one person, which
	// means a registry that is down could make a worker's history look shorter
	// than it is. The tempting behaviour — fall back to the single id — returns
	// a history with a hole and nothing anywhere saying something is missing,
	// and the caller believes it. On a system whose records decide whether
	// somebody gets paid, a read that fails loudly is worth more than one that
	// quietly under-reports.
	phone := sharedNumber(304)
	worker := newWorkerWithPhone(t, w, "Registry Outage", phone)
	res := w.submit(t, batch(row(phone, 2, "HH-outage-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %+v", res)
	}

	if err := harness.Kill(w.ctx, "registry"); err != nil {
		t.Fatalf("kill the registry: %v", err)
	}
	// Brought back whatever happens below, or every scenario after this one
	// fails for a reason that has nothing to do with what it was testing.
	t.Cleanup(func() {
		if err := harness.Start(context.Background(), "registry"); err != nil {
			t.Fatalf("restart the registry: %v", err)
		}
		if err := w.WaitReady(context.Background(), 90*time.Second); err != nil {
			t.Fatalf("the registry never came back: %v", err)
		}
	})

	code, body, err := w.Evidence.Status(w.ctx, http.MethodGet,
		"/v1/claims?partyId="+url.QueryEscape(worker), nil)
	if err != nil {
		t.Fatalf("list claims: %v", err)
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("with the registry down the claim list answered %d, not 503: %s", code, body)
	}
	if !strings.Contains(string(body), "registry_unavailable") {
		t.Fatalf("the refusal does not name the cause: %s", body)
	}
}
