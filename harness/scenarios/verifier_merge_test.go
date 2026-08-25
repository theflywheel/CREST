//go:build e2e

package scenarios

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
)

// What a verifier sees across a merge (#104, §16).
//
// The ruling: continuity wins, and the merge itself is not disclosed. A
// verifier resolving either of a merged person's ids gets the whole chain's
// credentials; the response never says two records were joined, because "this
// worker was recorded twice" is a fact about them they never volunteered — in
// some settings the reason is a migration, a name change, or a marriage, and a
// verifier checking one week's bednet distribution has no need of it.
//
// The residue is stated in §16 rather than hidden here: the credentials
// themselves name their subject party, so a verifier who reads the subjects of
// a chain's credentials can infer that two ids point at one person. What this
// scenario pins is that the SYSTEM adds nothing to that — no identifier list,
// no merged flag — and that the registry's own answer to "which ids are one
// person" is closed to anonymous callers.
func TestAVerifierSeesTheWholeChainsCredentialsAndNotTheMerge(t *testing.T) {
	w := setup(t)

	survivor, absorbed, hold := w.twoPartiesOneNumber(t, sharedNumber(305))
	claimA := w.rosterWork(t, survivor, "VRF-A-"+runID[:6], "HH-verif-a")
	claimB := w.rosterWork(t, absorbed, "VRF-B-"+runID[:6], "HH-verif-b")

	// Both sides confirm, so both sides hold a credential before the merge.
	for _, claimID := range []string{claimA, claimB} {
		eventually(t, "the window opens for "+claimID, 15*time.Second, func() error {
			_, err := w.window(claimID)
			return err
		})
		if err := w.confirmClaim(t, claimID, nil, nil); err != nil {
			t.Fatalf("confirm %s: %v", claimID, err)
		}
	}

	// Before the merge, resolving the survivor shows one credential. Asserted
	// so the assertion after the merge cannot pass by the endpoint returning
	// everything regardless.
	if n := len(w.chainCredentials(t, survivor)); n != 1 {
		t.Fatalf("before the merge the survivor resolves to %d credentials, want 1", n)
	}

	code, _ := w.resolveHold(t, hold.ID, map[string]any{
		"decision":           "merge",
		"partyId":            survivor,
		"resolvedByPartyId":  fixtures.CustodianID,
		"confirmedByPartyId": survivor,
		"confirmationMethod": "in-person",
	})
	if code != 200 {
		t.Fatalf("the merge was refused with %d", code)
	}

	// Either id resolves to the whole history. The absorbed id keeps working
	// — a verifier holding last year's bookmark must not be told the person
	// vanished.
	for _, id := range []string{survivor, absorbed} {
		if n := len(w.chainCredentials(t, id)); n != 2 {
			t.Fatalf("resolving %s returns %d credentials after the merge, want the chain's 2", id, n)
		}
	}

	// The negative half, which is the half that gets skipped: the response
	// body carries no merge vocabulary and no identifier mapping. The check is
	// on the raw bytes, not the decoded struct, so a field added later cannot
	// hide from it.
	code, raw, err := w.Verification.Status(w.ctx, http.MethodGet,
		"/v1/parties/"+url.PathEscape(survivor)+"/credentials", nil)
	if err != nil || code != http.StatusOK {
		t.Fatalf("read chain credentials: %d %v", code, err)
	}
	lower := strings.ToLower(string(raw))
	for _, word := range []string{"merged", "mergedinto", "identifiers", "absorbed"} {
		if strings.Contains(lower, word) {
			t.Fatalf("the verifier response discloses the merge (%q):\n%s", word, raw)
		}
	}

	// And the registry's own mapping is not anonymous reading. The worker (or
	// somebody acting for them) may ask; a stranger may not.
	code, _, err = w.Registry.Status(w.ctx, http.MethodGet,
		"/v1/parties/"+url.PathEscape(survivor)+"/identifiers", nil)
	if err != nil {
		t.Fatalf("read identifiers anonymously: %v", err)
	}
	if code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Fatalf("an anonymous caller read the identifier mapping (%d); that is the merge, disclosed", code)
	}

	// The worker themselves still can — the mapping is their own history.
	var ids struct {
		Identifiers []string `json:"identifiers"`
		Merged      bool     `json:"merged"`
	}
	if err := w.Registry.As(w.login(t, survivor)).Get(w.ctx,
		"/v1/parties/"+url.PathEscape(survivor)+"/identifiers", &ids); err != nil {
		t.Fatalf("the worker cannot read their own identifier history: %v", err)
	}
	if !ids.Merged || len(ids.Identifiers) != 2 {
		t.Fatalf("the worker's own view does not describe the merge: %+v", ids)
	}
}

// chainCredentials resolves a party through the verifier surface.
func (w *world) chainCredentials(t *testing.T, party string) []json.RawMessage {
	t.Helper()
	var out struct {
		Credentials []json.RawMessage `json:"credentials"`
	}
	if err := w.Verification.Get(w.ctx,
		"/v1/parties/"+url.PathEscape(party)+"/credentials", &out); err != nil {
		t.Fatalf("resolve %s through verification: %v", party, err)
	}
	return out.Credentials
}
