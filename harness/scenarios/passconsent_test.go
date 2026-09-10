//go:build e2e

package scenarios

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// A pass-holder asks a worker to see more (#27; J9 v1_2 "Request the check").
// The consent loop is the same one an onboarded institution walks — the worker
// decides per share, the asker collects exactly what was approved, once — and
// what the worker sees is a name, because a pass id is nothing they could
// resolve. A pass that did not ask cannot read or collect.
func TestAPassHolderAsksAndTheWorkerDecidesByName(t *testing.T) {
	w := setup(t)
	w.confirmedCredential(t, "HH-PASSASK-"+runID)
	pass := w.getPass(t, "Joseph Mwangi", "joseph."+strings.ToLower(runID)+"@example.org")
	asJoseph := w.Verification.As(harness.Caller{Pass: pass.Token})

	type view struct {
		Request struct {
			ID          string `json:"id"`
			RequestedBy string `json:"requestedByPartyId"`
		} `json:"request"`
		State          string `json:"state"`
		RequesterName  string `json:"requesterName"`
		RequesterKind  string `json:"requesterKind"`
		DisclosureList []struct {
			CredentialID string `json:"credentialId"`
		} `json:"disclosureList"`
	}

	// The ask names the pass; a body naming a party as well is one asker too many.
	code, body, _ := asJoseph.Status(w.ctx, http.MethodPost, "/v1/presentation-requests", map[string]any{
		"subjectPartyId": fixtures.WorkerAID, "requestedByPartyId": fixtures.OrgID, "purpose": "Hiring for a private clinic",
	})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "one_requester") {
		t.Fatalf("a pass naming a party as well was answered %d: %s", code, body)
	}
	var created view
	if err := asJoseph.Post(w.ctx, "/v1/presentation-requests", map[string]any{
		"subjectPartyId": fixtures.WorkerAID, "purpose": "Hiring for a private clinic",
	}, &created); err != nil {
		t.Fatalf("a pass-holder could not ask: %v", err)
	}
	if created.Request.RequestedBy != pass.Pass.ID || created.State != "REQUESTED" {
		t.Fatalf("the request is not the pass's own: %+v", created)
	}

	// The worker's face: who is asking, by name.
	worker := w.Verification.As(w.login(t, fixtures.WorkerAID))
	var seen view
	if err := worker.Get(w.ctx, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID), &seen); err != nil {
		t.Fatal(err)
	}
	if seen.RequesterName != "Joseph Mwangi" || seen.RequesterKind != "pass" {
		t.Fatalf("the worker is not told who is asking: %+v", seen)
	}
	if len(seen.DisclosureList) == 0 {
		t.Fatal("the worker was shown an empty disclosure list for a worker with a credential")
	}

	// A stranger's pass is a stranger: cannot read, cannot collect.
	other := w.getPass(t, "Somebody Else", "else."+strings.ToLower(runID)+"@example.org")
	asOther := w.Verification.As(harness.Caller{Pass: other.Token})
	if code, body, _ := asOther.Status(w.ctx, http.MethodGet, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID), nil); code != http.StatusForbidden {
		t.Errorf("another pass read the request: %d %s", code, body)
	}
	if code, body, _ := asOther.Status(w.ctx, http.MethodPost, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID)+"/collect", nil); code != http.StatusForbidden {
		t.Errorf("another pass collected the request: %d %s", code, body)
	}
	// Nor can the pass be named without being presented.
	if code, body, _ := w.Verification.Status(w.ctx, http.MethodGet,
		"/v1/presentation-requests?requestedByPartyId="+url.QueryEscape(pass.Pass.ID), nil); code != http.StatusUnauthorized {
		t.Errorf("a pass id was listed without the pass: %d %s", code, body)
	}

	// Collect before the decision is refused; the worker approves; the pass
	// collects exactly the approved list, once.
	if code, body, _ := asJoseph.Status(w.ctx, http.MethodPost, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID)+"/collect", nil); code != http.StatusConflict {
		t.Fatalf("a collect before the worker decided was answered %d: %s", code, body)
	}
	if err := worker.Post(w.ctx, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID)+"/decision", map[string]any{
		"approve": true, "approvedCredentialIds": []string{seen.DisclosureList[0].CredentialID},
	}, nil); err != nil {
		t.Fatalf("the worker could not decide: %v", err)
	}
	var got struct {
		Count       int              `json:"count"`
		Credentials []map[string]any `json:"credentials"`
	}
	if err := asJoseph.Post(w.ctx, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID)+"/collect", nil, &got); err != nil {
		t.Fatalf("the pass could not collect what was approved: %v", err)
	}
	if got.Count != 1 || len(got.Credentials) != 1 || got.Credentials[0]["id"] != seen.DisclosureList[0].CredentialID {
		t.Fatalf("collected something other than the approved list: %+v", got)
	}
	if code, _, _ := asJoseph.Status(w.ctx, http.MethodPost, "/v1/presentation-requests/"+url.PathEscape(created.Request.ID)+"/collect", nil); code != http.StatusConflict {
		t.Errorf("a second collect was answered %d, not 409", code)
	}

	// The pass lists its own asks, and the consented share is in the
	// worker's trail with the name on it.
	var mine struct {
		Requests []view `json:"requests"`
	}
	if err := asJoseph.Get(w.ctx, "/v1/presentation-requests?requestedByPartyId="+url.QueryEscape(pass.Pass.ID), &mine); err != nil {
		t.Fatal(err)
	}
	if len(mine.Requests) != 1 || mine.Requests[0].State != "FULFILLED" {
		t.Errorf("the pass's own list is wrong: %+v", mine.Requests)
	}
	var trail struct {
		Presentations []struct {
			RequestedBy   string `json:"requestedByPartyId"`
			RequesterName string `json:"requesterName"`
			Scope         string `json:"scope"`
		} `json:"presentations"`
	}
	if err := worker.Get(w.ctx, "/v1/presentations?subjectRef="+url.QueryEscape(fixtures.WorkerAID), &trail); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range trail.Presentations {
		if p.RequestedBy == pass.Pass.ID && p.Scope == "consented" && p.RequesterName == "Joseph Mwangi" {
			found = true
		}
	}
	if !found {
		t.Errorf("the consented share is not in the worker's trail under the pass-holder's name: %+v", trail.Presentations)
	}
}
