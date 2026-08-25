//go:build e2e

package scenarios

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// Batch verification (§16, #107): bounded, purposeful, and one trail entry per
// worker — never one per batch.

// confirmedCredential runs one claim to a credential and returns the signed
// document, which is what a batch caller would hold.
func (w *world) confirmedCredential(t *testing.T, household string) map[string]any {
	t.Helper()
	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	res := w.submit(t, batch(row(phone, 2, household)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("want one claim, got %+v", res)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.confirmClaim(t, claimID, nil, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	return w.credential(t, exit.Credential.ID)
}

func TestABatchCheckNeedsAPurposeAndStaysUnderTheCap(t *testing.T) {
	w := setup(t)

	credA := w.confirmedCredential(t, "HH-batch-a-"+runID)
	credB := w.confirmedCredential(t, "HH-batch-b-"+runID)

	// No purpose, no batch. A batch of checks with no stated purpose is a bulk
	// export with better manners.
	code, body, _ := w.Verification.Status(w.ctx, http.MethodPost, "/v1/verify/batch",
		map[string]any{
			"credentials":        []any{credA, credB},
			"requestedByPartyId": fixtures.OrgID,
		})
	if code != http.StatusBadRequest {
		t.Fatalf("a purposeless batch was answered %d, not 400: %s", code, body)
	}
	if !strings.Contains(string(body), "purpose_required") {
		t.Fatalf("the refusal does not name the rule: %s", body)
	}

	// Nor an anonymous one: "who checked me" must never answer nobody for the
	// largest checks.
	code, body, _ = w.Verification.Status(w.ctx, http.MethodPost, "/v1/verify/batch",
		map[string]any{
			"credentials": []any{credA, credB},
			"purpose":     "Q1 payroll audit, Riverside district",
		})
	if code != http.StatusBadRequest {
		t.Fatalf("an anonymous batch was answered %d, not 400: %s", code, body)
	}

	// Over the cap, refused — with the number, so the caller knows what to
	// take up with the deployment rather than what to retry.
	over := make([]any, 101)
	for i := range over {
		over[i] = credA
	}
	code, body, _ = w.Verification.Status(w.ctx, http.MethodPost, "/v1/verify/batch",
		map[string]any{
			"credentials":        over,
			"requestedByPartyId": fixtures.OrgID,
			"purpose":            "Q1 payroll audit, Riverside district",
		})
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an over-cap batch was answered %d, not 413: %s", code, body)
	}
	if !strings.Contains(string(body), "batch_over_cap") {
		t.Fatalf("the refusal does not name the cap: %s", body)
	}

	// A bounded, purposeful batch answers per credential.
	var out struct {
		Verdicts []struct {
			Valid bool `json:"valid"`
		} `json:"verdicts"`
		Count int `json:"count"`
	}
	if err := w.Verification.Post(w.ctx, "/v1/verify/batch", map[string]any{
		"credentials":        []any{credA, credB},
		"requestedByPartyId": fixtures.OrgID,
		"purpose":            "Q1 payroll audit, Riverside district",
	}, &out); err != nil {
		t.Fatalf("batch verify: %v", err)
	}
	if out.Count != 2 || len(out.Verdicts) != 2 {
		t.Fatalf("want 2 verdicts, got %+v", out)
	}
	for i, v := range out.Verdicts {
		if !v.Valid {
			t.Fatalf("credential %d in the batch did not verify", i)
		}
	}

	// One trail entry per worker. The worker-facing answer to "who checked me"
	// must read the same whether the check arrived alone or among a hundred.
	var trail struct {
		Presentations []struct {
			Purpose     string `json:"purpose"`
			RequestedBy string `json:"requestedByPartyId"`
		} `json:"presentations"`
	}
	if err := w.Verification.Get(w.ctx,
		"/v1/presentations?subjectRef="+url.QueryEscape(fixtures.WorkerAID), &trail); err != nil {
		t.Fatalf("read the check trail: %v", err)
	}
	batchEntries := 0
	for _, p := range trail.Presentations {
		if p.Purpose == "Q1 payroll audit, Riverside district" && p.RequestedBy == fixtures.OrgID {
			batchEntries++
		}
	}
	if batchEntries < 2 {
		t.Fatalf("the batch left %d entries in the worker's trail, want one per credential (2); "+
			"a single entry for a whole batch makes \"who checked me\" silently wrong for the "+
			"largest checks", batchEntries)
	}
}
