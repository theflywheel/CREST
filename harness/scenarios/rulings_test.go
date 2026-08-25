//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// The §16 rulings that change what the registry answers (#105's successors):
// review-by dates on authorizations, and grant narrowing after go-live.

// grantSubmitter creates a fresh party holding submit-work-evidence in the
// fixture project, so a scenario can revoke or age a grant without touching
// the supervisor's — whose grant every other scenario depends on.
func (w *world) grantSubmitter(t *testing.T, name string, reviewBy *time.Time) (string, string) {
	t.Helper()
	party := w.newWorker(t, name)
	epoch := w.w.Instance.Epoch
	end := epoch.Add(300 * 24 * time.Hour)
	auth := schema.Authorization{
		PartyID: party,
		Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
		Scope: schema.AuthorizationScope{
			Kind:      schema.AuthorizationScopeKindContext,
			ContextID: ptr(fixtures.ProjectID),
		},
		Functions:         []string{"submit-work-evidence"},
		Period:            schema.Period{Start: epoch.Add(-30 * 24 * time.Hour), End: &end},
		ReviewBy:          reviewBy,
		AuthorityPartyID:  fixtures.OrgID,
		ApprovedByPartyID: fixtures.OrgID,
		ApprovedAt:        epoch.Add(-30 * 24 * time.Hour),
		State:             schema.AuthorizationStateACTIVE,
	}
	var created schema.Authorization
	if err := w.Parties.Post(w.ctx, "/v1/authorizations", auth, &created); err != nil {
		t.Fatalf("create authorization: %v", err)
	}
	return party, created.ID
}

// submitAs is submit with the submitter chosen by the scenario.
func (w *world) submitAs(t *testing.T, submitter string, csv []byte) (int, []byte) {
	t.Helper()
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=dhis2-riverside",
		fixtures.ProjectID, fixtures.DefinitionID, url.QueryEscape(submitter))
	code, body, err := w.Evidence.As(w.login(t, submitter)).StatusRaw(w.ctx, http.MethodPost, path, "text/csv", csv)
	if err != nil {
		t.Fatalf("submit as %s: %v", submitter, err)
	}
	return code, body
}

func ptr[T any](v T) *T { return &v }

func mustDecode(t *testing.T, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

// §16: when a review-by date passes, the authorization keeps working and
// becomes visibly overdue. Both halves are asserted, because the second
// without the first would be the rejected ruling — a lapse that silently
// stops paying a worker for an administrator's missed calendar entry.
func TestAnOverdueAuthorizationStillPermitsAndIsListedForReview(t *testing.T) {
	w := setup(t)

	overdueSince := w.w.Instance.Epoch.Add(-10 * 24 * time.Hour)
	submitter, authID := w.grantSubmitter(t, "Overdue Grant Holder "+runID, &overdueSince)

	// The grant still answers yes — and says it is overdue.
	var perm struct {
		Permitted bool `json:"permitted"`
		Overdue   bool `json:"overdue"`
	}
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, fmt.Sprintf(
		"/v1/authorizations/permits?partyId=%s&function=submit-work-evidence&contextId=%s",
		url.QueryEscape(submitter), url.QueryEscape(fixtures.ProjectID)), &perm); err != nil {
		t.Fatalf("permits: %v", err)
	}
	if !perm.Permitted {
		t.Fatalf("an overdue authorization stopped permitting; that is the rejected ruling, "+
			"where a missed review withholds a worker's payment: %+v", perm)
	}
	if !perm.Overdue {
		t.Fatalf("the permits answer does not surface the overdue review: %+v", perm)
	}

	// Work still enters under it.
	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	code, body := w.submitAs(t, submitter, batch(row(phone, 2, "HH-overdue-"+runID)))
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("a submission under an overdue-but-active grant was refused with %d: %s", code, body)
	}

	// And somebody can find it: the review queue names the authorization.
	var list struct {
		Authorizations []schema.Authorization `json:"authorizations"`
	}
	if err := w.Parties.Get(w.ctx, "/v1/authorizations/overdue", &list); err != nil {
		t.Fatalf("list overdue: %v", err)
	}
	found := false
	for _, a := range list.Authorizations {
		if a.ID == authID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the overdue authorization is not on the review list; overdue-but-working "+
			"only holds if somebody can see the overdue half (%d listed)", len(list.Authorizations))
	}
}

// §16 (J3): narrowing binds at submission. Work already submitted under a
// grant confirms and pays after the grant is revoked; new work is refused.
// Both halves in one scenario, because each alone can pass for the wrong
// reason — the first because nothing re-checks anywhere, the second because
// nothing was ever granted.
func TestARevokedGrantStopsNewWorkButNotWorkInFlight(t *testing.T) {
	w := setup(t)

	submitter, authID := w.grantSubmitter(t, "Narrowed Grant Holder "+runID, nil)
	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)

	code, body := w.submitAs(t, submitter, batch(row(phone, 3, "HH-narrow-"+runID)))
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("the pre-narrowing submission failed with %d: %s", code, body)
	}
	var res ingestResult
	mustDecode(t, body, &res)
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("want one claim in flight, got %+v", res)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	// The narrowing.
	if err := w.Parties.Post(w.ctx,
		"/v1/authorizations/"+url.PathEscape(authID)+"/revoke", nil, nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// New work is refused at the door.
	code, body = w.submitAs(t, submitter, batch(row(phone, 1, "HH-after-"+runID)))
	if code < 400 {
		t.Fatalf("a submission under a revoked grant was accepted (%d): %s\n"+
			"Narrowing that does not stop new work is not narrowing.", code, body)
	}

	// The work in flight completes: the worker confirms, the payment releases.
	// The check ran when the work entered; the worker cannot un-perform it.
	if err := w.confirmClaim(t, claimID, nil, nil); err != nil {
		t.Fatalf("confirming in-flight work after the grant was narrowed failed: %v\n"+
			"That strands already-performed work unpaid, which is the rejected ruling.", err)
	}
	eventually(t, "the payment for in-flight work is released", 20*time.Second, func() error {
		_, err := w.instruction(claimID)
		return err
	})
}
