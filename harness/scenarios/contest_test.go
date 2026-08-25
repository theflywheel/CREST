//go:build e2e

package scenarios

import (
	"net/http"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// A dispute is metadata on the credential, never a revocation (#58).
//
// The decision, and the reason for it: a credential is a historical statement —
// what was asserted, by whom, and when. Revoking it because the worker later
// objected would mean a worker who disputes one detail ("it was six, not nine")
// loses the whole record of work they did do. That is a penalty for objecting,
// and it falls on exactly the person the dispute exists to protect.
//
// So the credential stands and the dispute is visible beside it. Before this,
// the behaviour was the credential standing with the dispute invisible — which
// #58 called the one combination nobody would pick deliberately.

func (w *world) issueThenDispute(t *testing.T, household string) (string, string) {
	t.Helper()
	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	res := w.submit(t, batch(row(phone, 9, household+"-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %d: %+v", len(res.ClaimIDs), res.Unclear)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	// Confirmed first, so the credential exists before the dispute does. This
	// is the case the issue is about: a claim can go ACCEPTED → DISPUTED after
	// a credential was signed, because the seven days are a window for
	// objecting rather than a deadline for noticing (W3).
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.confirmClaim(t, claimID,
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if exit.Credential.ID == "" {
		t.Fatal("confirming produced no credential")
	}
	return claimID, exit.Credential.ID
}

func TestADisputeDoesNotRevokeTheCredential(t *testing.T) {
	w := setup(t)
	claimID, credID := w.issueThenDispute(t, "HH-DISPUTE")

	before := w.verify(t, w.credential(t, credID))
	if !before.Valid || len(before.Contested) != 0 {
		t.Fatalf("before any dispute: valid=%v contested=%v", before.Valid, before.Contested)
	}

	if err := w.disputeClaim(t, claimID, map[string]any{
		"reason":          "the household count is wrong; it was six, not nine",
		"raisedByPartyId": fixtures.WorkerAID,
	}, nil); err != nil {
		t.Fatalf("dispute: %v", err)
	}

	after := w.verify(t, w.credential(t, credID))

	// The credential is untouched. valid means "this issuer really asserted
	// this and has not withdrawn it" — a question about the document — and a
	// dispute is a question about the world.
	if !after.Valid {
		t.Errorf("disputing the claim invalidated the credential: %v", after.Reasons)
	}
	if after.Revoked {
		t.Error("disputing the claim flipped the status list bit; a dispute is not a withdrawal")
	}

	// And it is visible, which is the half that was missing.
	if len(after.Contested) == 0 {
		t.Fatal("the claim is disputed and the verdict does not say so; a verifier sees a clean record")
	}
	c := after.Contested[0]
	if c.State != string(schema.ContestStateOPEN) {
		t.Errorf("contest state = %q, want OPEN", c.State)
	}
	if c.Against != "claim" {
		t.Errorf("contest against = %q, want claim", c.Against)
	}
	t.Logf("credential still valid, and the verdict reports the dispute: %+v", c)
}

// The worker's own words are not a verifier's business.
//
// A dispute reason is ordinarily the worker saying something about their own
// record. A verifier's legitimate interest is that the record is contested and
// where that stands — not what the worker said, and not that it was the worker
// who said it.
func TestAVerifierLearnsTheStandingNotTheReason(t *testing.T) {
	w := setup(t)
	claimID, credID := w.issueThenDispute(t, "HH-PRIVATE")

	const reason = "my supervisor recorded this against the wrong person"
	if err := w.disputeClaim(t, claimID, map[string]any{
		"reason": reason, "raisedByPartyId": fixtures.WorkerAID,
	}, nil); err != nil {
		t.Fatalf("dispute: %v", err)
	}

	// Asserted on the serialised verdict, not on the struct: a field that never
	// got a Go name can still be in the JSON.
	raw := w.verifyRaw(t, w.credential(t, credID))
	for what, needle := range map[string]string{
		"the reason":       reason,
		"the worker's id":  fixtures.WorkerAID,
		"the word 'wrong'": "wrong person",
	} {
		if containsStr(raw, needle) {
			t.Errorf("%s reached the verifier: %s", what, raw)
		}
	}

	// The same is true of the endpoint itself, which is what a console would
	// call directly.
	code, body, err := w.Confirmation.Status(w.ctx, http.MethodGet,
		"/v1/contests?targetKind=claim&targetId="+claimID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK {
		t.Fatalf("contests answered %d: %s", code, body)
	}
	if containsStr(string(body), reason) || containsStr(string(body), fixtures.WorkerAID) {
		t.Errorf("the contests endpoint returned the reason or the raiser: %s", body)
	}
}

// An unscoped listing would be a search for disputed workers.
func TestContestsCannotBeListedUnscoped(t *testing.T) {
	w := setup(t)
	for _, q := range []string{"", "?targetKind=claim", "?targetId=x", "?targetKind=unit&targetId=x"} {
		code, body, err := w.Confirmation.Status(w.ctx, http.MethodGet, "/v1/contests"+q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if code != http.StatusBadRequest {
			t.Errorf("GET /v1/contests%s answered %d: %s", q, code, body)
		}
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
