//go:build e2e

package scenarios

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
)

// The verifier pass and G1's three rules, on real services (#27, #9).
//
// A stranger gets a pass with a name and a contact and nothing else; every
// check they make is recorded against it where the worker can see the name;
// the pass cannot batch; checking in bulk needs an authorization somebody
// granted; and no requester — pass or party — gets more checks in a window than
// the deployment allows. The three rules carry each other: the trail is what
// the cap is counted from, and the cap is what makes the trail a control
// rather than a receipt.

type passIssued struct {
	Pass struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		IssuedAt string `json:"issuedAt"`
		Rotated  bool   `json:"rotated"`
	} `json:"pass"`
	Token   string `json:"token"`
	Header  string `json:"header"`
	RateCap struct {
		Checks int    `json:"checks"`
		Window string `json:"window"`
	} `json:"rateCap"`
}

func (w *world) getPass(t *testing.T, name, contact string) passIssued {
	t.Helper()
	var out passIssued
	if err := w.Verification.Post(w.ctx, "/v1/verifier-passes", map[string]any{
		"name": name, "contact": contact,
	}, &out); err != nil {
		t.Fatalf("issue a pass: %v", err)
	}
	if !strings.HasPrefix(out.Pass.ID, "crest:pass:") || out.Token == "" {
		t.Fatalf("a pass came back without an id or a token: %+v", out)
	}
	return out
}

func TestAStrangerChecksWithAPassAndTheWorkerSeesTheName(t *testing.T) {
	w := setup(t)
	cred := w.confirmedCredential(t, "HH-PASS-"+runID)

	// Nobody behind the check: refused, and told how to stop being nobody.
	code, body, err := w.Verification.Status(w.ctx, http.MethodPost, "/v1/verify",
		map[string]any{"credential": cred})
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusUnauthorized || !strings.Contains(string(body), "pass_required") {
		t.Fatalf("an anonymous online check was answered %d, not 401 pass_required: %s", code, body)
	}

	// A pass: a name and a contact, no account, no vetting.
	pass := w.getPass(t, "Joseph Mwangi", "+254 700 000 "+runID[len(runID)-3:])
	asJoseph := w.Verification.As(harness.Caller{Pass: pass.Token})

	var v verdict
	if err := asJoseph.Post(w.ctx, "/v1/verify", map[string]any{
		"credential": cred, "purpose": "Hiring for a private clinic",
	}, &v); err != nil {
		t.Fatalf("a pass-holder's check failed: %v", err)
	}
	if !v.Valid {
		t.Fatalf("the credential did not verify for the pass-holder: %v", v.Reasons)
	}

	// A pass or a party, not both.
	code, body, _ = asJoseph.Status(w.ctx, http.MethodPost, "/v1/verify",
		map[string]any{"credential": cred, "requestedByPartyId": fixtures.OrgID})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "one_requester") {
		t.Errorf("a check naming a pass and a party was answered %d: %s", code, body)
	}

	// The worker's trail names the stranger — by name, not only by an id
	// they cannot resolve anywhere (W8).
	var trail struct {
		Presentations []struct {
			RequestedBy   string `json:"requestedByPartyId"`
			RequesterName string `json:"requesterName"`
			Purpose       string `json:"purpose"`
			Scope         string `json:"scope"`
		} `json:"presentations"`
	}
	if err := w.Verification.As(w.login(t, fixtures.WorkerAID)).Get(w.ctx,
		"/v1/presentations?subjectRef="+url.QueryEscape(fixtures.WorkerAID), &trail); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range trail.Presentations {
		if p.RequestedBy == pass.Pass.ID {
			found = true
			if p.RequesterName != "Joseph Mwangi" || p.Scope != "pass" || p.Purpose != "Hiring for a private clinic" {
				t.Errorf("the trail row does not tell the worker who looked: %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("the pass-holder's check left no line in the worker's trail: %+v", trail.Presentations)
	}

	// A pass never batches.
	code, body, _ = asJoseph.Status(w.ctx, http.MethodPost, "/v1/verify/batch", map[string]any{
		"credentials": []any{cred}, "purpose": "Hiring for a private clinic",
	})
	if code != http.StatusForbidden || !strings.Contains(string(body), "pass_cannot_batch") {
		t.Errorf("a pass-holder's batch was answered %d: %s", code, body)
	}

	// Asking again with the same contact rotates the token; it does not mint
	// a second identity to count against.
	again := w.getPass(t, "J. Mwangi", "+254700000"+runID[len(runID)-3:])
	if again.Pass.ID != pass.Pass.ID || !again.Pass.Rotated {
		t.Errorf("asking again minted a second pass: %+v then %+v", pass.Pass, again.Pass)
	}
	code, body, _ = asJoseph.Status(w.ctx, http.MethodPost, "/v1/verify", map[string]any{"credential": cred})
	if code != http.StatusUnauthorized || !strings.Contains(string(body), "invalid_pass") {
		t.Errorf("the replaced token still checks: %d %s", code, body)
	}
}

func TestBulkCheckingIsAScopeAnAuthorityGrants(t *testing.T) {
	w := setup(t)
	cred := w.confirmedCredential(t, "HH-BULK-"+runID)
	batch := func(party string) (int, string) {
		code, body, err := w.Verification.As(w.login(t, party)).Status(w.ctx, http.MethodPost, "/v1/verify/batch",
			map[string]any{
				"credentials": []any{cred}, "requestedByPartyId": party,
				"purpose": "Q1 payroll audit, Riverside district",
			})
		if err != nil {
			t.Fatal(err)
		}
		return code, string(body)
	}

	// Signed in, named, purposeful — and still refused, because nobody
	// granted this party the scope. A sign-in is not a licence to sweep.
	if code, body := batch(fixtures.SpecifierID); code != http.StatusForbidden || !strings.Contains(body, "bulk_scope_required") {
		t.Fatalf("a party with no bulk authorization batched: %d %s", code, body)
	}
	// The organisation holds verify-credentials-bulk (fixture VBKX), so it may.
	if code, body := batch(fixtures.OrgID); code != http.StatusOK {
		t.Fatalf("the organisation holding the scope was refused: %d %s", code, body)
	}
}

// The rate cap, end to end: a fresh pass checks up to the deployment's cap and
// is then refused, with the number and the wait named. The cap is read back
// from the deployment rather than typed here, so this holds at whatever a
// stack was configured with.
func TestARequesterIsCappedAtTheDeploymentsRate(t *testing.T) {
	w := setup(t)
	cred := w.confirmedCredential(t, "HH-RATE-"+runID)
	pass := w.getPass(t, "Ruth Njeri", "ruth."+strings.ToLower(runID)+"@example.org")
	if pass.RateCap.Checks <= 0 || pass.RateCap.Checks > 500 {
		t.Fatalf("the deployment's cap is %d; this scenario expects a bounded, positive one", pass.RateCap.Checks)
	}
	asRuth := w.Verification.As(harness.Caller{Pass: pass.Token})

	deadline := time.Now().Add(harness.Patience(2 * time.Minute))
	for i := 1; i <= pass.RateCap.Checks; i++ {
		if time.Now().After(deadline) {
			t.Fatalf("checking %d credentials took longer than %s", pass.RateCap.Checks, harness.Patience(2*time.Minute))
		}
		code, body, hdr, err := asRuth.StatusWithHeaders(w.ctx, http.MethodPost, "/v1/verify", map[string]any{"credential": cred})
		if err != nil {
			t.Fatal(err)
		}
		if code != http.StatusOK {
			t.Fatalf("check %d of %d was answered %d: %s", i, pass.RateCap.Checks, code, body)
		}
		if rem, _ := strconv.Atoi(hdr.Get("X-CREST-Rate-Remaining")); rem != pass.RateCap.Checks-i {
			t.Fatalf("after check %d the deployment says %s remain, want %d", i, hdr.Get("X-CREST-Rate-Remaining"), pass.RateCap.Checks-i)
		}
	}
	code, body, hdr, _ := asRuth.StatusWithHeaders(w.ctx, http.MethodPost, "/v1/verify", map[string]any{"credential": cred})
	if code != http.StatusTooManyRequests || !strings.Contains(string(body), "rate_cap_exceeded") {
		t.Fatalf("check %d was answered %d, not 429 rate_cap_exceeded: %s", pass.RateCap.Checks+1, code, body)
	}
	if hdr.Get("Retry-After") == "" {
		t.Error("the refusal does not say how long to wait")
	}
	// Another pass is another person's allowance; the cap is per requester,
	// not per deployment.
	other := w.getPass(t, "Another Verifier", "other."+strings.ToLower(runID)+"@example.org")
	if code, body, _ := w.Verification.As(harness.Caller{Pass: other.Token}).Status(w.ctx, http.MethodPost, "/v1/verify",
		map[string]any{"credential": cred}); code != http.StatusOK {
		t.Errorf("one pass reaching the cap capped another: %d %s", code, body)
	}
}
