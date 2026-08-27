//go:build e2e

package scenarios

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/pkg/schema"
)

// The first-login bootstrap accepts a self-bind on a never-bound party. Two
// strangers racing the same unbound party must not both win: the self-proof
// decision is re-made inside the append transaction under a row lock, so the
// loser sees a party that is no longer unbound and is refused. Without that,
// both callers read "never bound", both append, and one login authenticates
// as a party somebody else just claimed.
func TestTwoFirstLoginsRacingOneUnboundPartyBindOnce(t *testing.T) {
	w := setup(t)

	party := w.newWorker(t, "Claimed Twice "+runID)

	bind := func(sub string) (int, error) {
		token, err := w.oidc.Token(w.ctx, sub)
		if err != nil {
			return 0, fmt.Errorf("mint token for %s: %w", sub, err)
		}
		code, _, err := w.Parties.As(harness.Caller{Token: token}).Status(w.ctx,
			http.MethodPost, "/v1/parties/"+party+"/identity-bindings", map[string]any{
				"provider":      "mock-oidc",
				"providerClass": "generic-oidc",
				"subjectRef":    w.oidc.Subject(sub),
			})
		return code, err
	}

	subA := "race-a|" + runID
	subB := "race-b|" + runID
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(2)
	var codeA, codeB int
	var errA, errB error
	go func() { defer done.Done(); start.Wait(); codeA, errA = bind(subA) }()
	go func() { defer done.Done(); start.Wait(); codeB, errB = bind(subB) }()
	start.Done()
	done.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("bind attempts errored: %v / %v", errA, errB)
	}

	wins := 0
	for _, code := range []int{codeA, codeB} {
		switch {
		case code >= 200 && code < 300:
			wins++
		case code == http.StatusForbidden:
			// The loser's refusal.
		default:
			t.Fatalf("a racing first login answered %d, want 2xx or 403 (got %d and %d)",
				code, codeA, codeB)
		}
	}
	if wins != 1 {
		t.Fatalf("%d of two racing first logins claimed the party, want exactly 1 (%d and %d)",
			wins, codeA, codeB)
	}

	// The record agrees: one authenticating binding, not two.
	winner := subA
	if codeB >= 200 && codeB < 300 {
		winner = subB
	}
	token, err := w.oidc.Token(w.ctx, winner)
	if err != nil {
		t.Fatalf("re-mint the winner's token: %v", err)
	}
	var p schema.Party
	if err := w.Parties.As(harness.Caller{Token: token}).
		Get(w.ctx, "/v1/parties/"+party, &p); err != nil {
		t.Fatalf("read the party as its winner: %v", err)
	}
	bound := 0
	for _, b := range p.IdentityBindings {
		if b.Provider == "mock-oidc" {
			bound++
		}
	}
	if bound != 1 {
		t.Fatalf("the race left %d bindings on the party, want exactly 1: %+v",
			bound, p.IdentityBindings)
	}
}
