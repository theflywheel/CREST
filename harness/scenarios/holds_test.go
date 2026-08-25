//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// Closing a duplicate hold (§4, W7).
//
// The registry has always refused to merge on its own: two parties carrying one
// identifier produce a hold and a 409, never a guess. What did not exist was any
// way to close one, and that has a consequence the manifest should not have been
// allowed to state as a pass — `merges_without_confirmation = 0` read zero
// because merging was impossible, not because the control worked.
//
// These scenarios exercise both ways a hold ends, and the refusals in between.

type holdView struct {
	ID         string   `json:"id"`
	KeyKind    string   `json:"keyKind"`
	KeyValue   string   `json:"keyValue"`
	Candidates []string `json:"candidates"`
	Reason     string   `json:"reason"`
}

type holdResolution struct {
	HoldID             string    `json:"holdId"`
	Decision           string    `json:"decision"`
	PartyID            string    `json:"partyId"`
	ResolvedBy         string    `json:"resolvedBy"`
	ResolvedAt         time.Time `json:"resolvedAt"`
	Merged             []string  `json:"merged"`
	ConfirmedBy        string    `json:"confirmedBy"`
	ConfirmationMethod string    `json:"confirmationMethod"`
}

// sharedNumber is a phone number unique to this run.
//
// Hard-coding one would make these scenarios pass only against a fresh
// database: the parties created for +1555017700x survive the run, and the next
// one finds three candidates on a hold that expects two. The suite already made
// this decision for batches (see runID) — a suite that only passes on a fresh
// database is a suite that gets run less.
func sharedNumber(seq int) string {
	n, err := strconv.ParseInt(runID[:8], 16, 64)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("+1555%07d", (n+int64(seq))%10_000_000)
}

// twoPartiesOneNumber is the case no fixture had: a shared phone. It is not
// exotic — one handset per household is the normal arrangement in the first use
// case, which is exactly why the registry must not merge on it.
func (w *world) twoPartiesOneNumber(t *testing.T, phone string) (string, string, holdView) {
	t.Helper()
	ids := make([]string, 0, 2)
	for _, name := range []string{"Shared Handset A ", "Shared Handset B "} {
		var created schema.Party
		if err := w.Parties.Post(w.ctx, "/v1/parties", schema.Party{
			Kind:        schema.PartyKindPerson,
			DisplayName: name + runID,
			ContactRoutes: []schema.PartyContactRoutesItem{
				{Kind: schema.PartyContactRoutesItemKindPhone, Value: phone},
			},
		}, &created); err != nil {
			t.Fatalf("create party: %v", err)
		}
		ids = append(ids, created.ID)
	}

	// Resolving now must be a 409 with a hold, not a pick.
	code, body, err := w.Parties.Status(w.ctx, http.MethodGet,
		fmt.Sprintf("/v1/resolve?kind=contact-route&value=%s&contextId=%s",
			url.QueryEscape(phone), fixtures.ProjectID), nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict {
		t.Fatalf("two parties on one number resolved with %d; the registry picked one", code)
	}
	var hold holdView
	if err := json.Unmarshal(body, &hold); err != nil {
		t.Fatalf("read the hold: %v (%s)", err, body)
	}
	return ids[0], ids[1], hold
}

func (w *world) resolveHold(t *testing.T, holdID string, body map[string]any) (int, holdResolution) {
	t.Helper()
	var out holdResolution
	code, raw, err := w.Parties.Status(w.ctx, http.MethodPost, "/v1/holds/"+holdID+"/resolve", body)
	if err != nil {
		t.Fatalf("resolve hold: %v", err)
	}
	if code == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("read the resolution: %v (%s)", err, raw)
		}
	}
	return code, out
}

// The refusal §4 exists for. A merge on a custodian's judgement alone is what
// W7 forbids, and the API must have no shape that expresses one.
func TestAMergeWithoutTheWorkersConfirmationCannotBeExpressed(t *testing.T) {
	w := setup(t)
	a, _, hold := w.twoPartiesOneNumber(t, sharedNumber(1))

	code, _ := w.resolveHold(t, hold.ID, map[string]any{
		"decision": "merge", "partyId": a, "resolvedByPartyId": fixtures.CustodianID,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("an unconfirmed merge was accepted with %d", code)
	}

	// And the count that is supposed to watch for this is still zero — for the
	// right reason now, since the endpoint exists and refused.
	var metrics struct {
		MergesWithoutConfirmation int `json:"mergesWithoutConfirmation"`
	}
	if err := w.Parties.Get(w.ctx, "/v1/holds/metrics", &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.MergesWithoutConfirmation != 0 {
		t.Errorf("merges_without_confirmation = %d", metrics.MergesWithoutConfirmation)
	}
}

// One person recorded twice: the merge is taken, the absorbed party is not
// deleted, and the identifier resolves to the survivor from then on.
func TestAConfirmedMergeMakesTheIdentifierResolveToTheSurvivor(t *testing.T) {
	w := setup(t)
	phone := sharedNumber(2)
	survivor, absorbed, hold := w.twoPartiesOneNumber(t, phone)

	code, res := w.resolveHold(t, hold.ID, map[string]any{
		"decision": "merge", "partyId": survivor, "resolvedByPartyId": fixtures.CustodianID,
		"confirmedByPartyId": survivor, "confirmationMethod": "voice",
	})
	if code != http.StatusOK {
		t.Fatalf("a confirmed merge was refused with %d", code)
	}
	if len(res.Merged) != 1 || res.Merged[0] != absorbed {
		t.Fatalf("merged %v, want [%s]", res.Merged, absorbed)
	}

	// The identifier now resolves, rather than holding: the duplicate is
	// decided, and a custodian must not be asked the same question again on
	// the next batch.
	var match struct {
		PartyID string `json:"partyId"`
	}
	if err := w.Parties.Get(w.ctx,
		fmt.Sprintf("/v1/resolve?kind=contact-route&value=%s&contextId=%s",
			url.QueryEscape(phone), fixtures.ProjectID), &match); err != nil {
		t.Fatalf("resolve after merge: %v", err)
	}
	if match.PartyID != survivor {
		t.Errorf("resolved to %s, want the survivor %s", match.PartyID, survivor)
	}

	// The absorbed party still exists and says where it went. Deleting it would
	// put a hole in a worker's history exactly where the system corrected its
	// own mistake about who they were.
	var gone schema.Party
	if err := w.Parties.Get(w.ctx, "/v1/parties/"+absorbed, &gone); err != nil {
		t.Fatalf("the absorbed party was deleted: %v", err)
	}
	if gone.MergedInto == nil || *gone.MergedInto != survivor {
		t.Errorf("the absorbed party does not point at the survivor: %+v", gone.MergedInto)
	}
}

// Two people, one handset. Nothing about either of them changes except that the
// number stops being a way to find the one it does not belong to.
func TestTwoPeopleSharingAPhoneAreNotMerged(t *testing.T) {
	w := setup(t)
	phone := sharedNumber(3)
	owner, other, hold := w.twoPartiesOneNumber(t, phone)

	code, res := w.resolveHold(t, hold.ID, map[string]any{
		"decision": "distinct", "partyId": owner, "resolvedByPartyId": fixtures.CustodianID,
	})
	if code != http.StatusOK {
		t.Fatalf("a distinct decision was refused with %d", code)
	}
	if len(res.Merged) != 0 {
		t.Errorf("a distinct decision merged %v", res.Merged)
	}

	var match struct {
		PartyID string `json:"partyId"`
	}
	if err := w.Parties.Get(w.ctx,
		fmt.Sprintf("/v1/resolve?kind=contact-route&value=%s&contextId=%s",
			url.QueryEscape(phone), fixtures.ProjectID), &match); err != nil {
		t.Fatalf("resolve after distinct: %v", err)
	}
	if match.PartyID != owner {
		t.Errorf("resolved to %s, want %s", match.PartyID, owner)
	}

	// The other person is untouched — still there, not merged, still findable
	// by everything else about them. They shared a handset; that is not a
	// reason to take their record apart.
	var still schema.Party
	if err := w.Parties.Get(w.ctx, "/v1/parties/"+other, &still); err != nil {
		t.Fatalf("the other party disappeared: %v", err)
	}
	if still.MergedInto != nil {
		t.Errorf("a distinct decision merged the other party into %s", *still.MergedInto)
	}
}

// A custodian confirming on the worker's behalf is the thing being guarded
// against, so the queue must not disclose more than the decision needs. §16
// asks for existence, not content — and the content here is the phone number of
// every worker two records disagree about.
func TestTheHoldQueueDoesNotHandOutTheCollidingIdentifier(t *testing.T) {
	w := setup(t)
	phone := sharedNumber(4)
	w.twoPartiesOneNumber(t, phone)

	code, raw, err := w.Parties.Status(w.ctx, http.MethodGet, "/v1/holds", nil)
	if err != nil || code != http.StatusOK {
		t.Fatalf("list holds: %d %v", code, err)
	}
	if bytesContain(raw, phone) {
		t.Errorf("the hold queue disclosed the colliding phone number")
	}

	var list struct {
		Holds []holdView `json:"holds"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Holds) == 0 {
		t.Fatal("no holds listed")
	}
	for _, h := range list.Holds {
		if h.KeyKind == "" || len(h.Candidates) < 2 {
			t.Errorf("a hold with no kind or fewer than two candidates cannot be decided: %+v", h)
		}
	}
}

// A hold is closed by choosing among its candidates. Naming a third party is
// not a decision about this hold, and accepting it would attach an identifier
// to somebody the registry never found it on.
func TestAHoldCannotBeResolvedOntoSomebodyElse(t *testing.T) {
	w := setup(t)
	_, _, hold := w.twoPartiesOneNumber(t, sharedNumber(5))

	code, _ := w.resolveHold(t, hold.ID, map[string]any{
		"decision": "distinct", "partyId": fixtures.WorkerAID, "resolvedByPartyId": fixtures.CustodianID,
	})
	if code != http.StatusConflict {
		t.Errorf("a hold was resolved onto a party that was never a candidate: %d", code)
	}
}

func bytesContain(haystack []byte, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if string(haystack[i:i+len(needle)]) == needle {
					return true
				}
			}
			return false
		}()
}
