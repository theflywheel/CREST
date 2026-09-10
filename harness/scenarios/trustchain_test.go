//go:build e2e

package scenarios

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
)

// The trust chain above the definition, walked on a real credential (#27,
// Blueprint §3, §8): from the credential's issuerAuthority through the
// organisation's two authorizations and the organisation itself to the
// deployment that operates the registry. #16 proved the credential NAMES the
// chain; this proves a verdict WALKS it — every link the credential names is
// checked against what the registry published, and each link tells the
// verifier where to check it themselves or what they are taking on trust.
func TestAVerdictWalksTheChainFromTheCredentialToTheDeployment(t *testing.T) {
	w := setup(t)
	cred := w.confirmedCredential(t, "HH-WALK-"+runID)

	// The chain is only walkable once its facts have reached the registry.
	// The fixture world's organisation and its authorizations are published
	// at bootstrap; wait for them rather than assume the relay beat us here.
	for _, ref := range []string{"authorization/" + fixtures.OrgQualificationID, "authorization/" + fixtures.OrgGrantID, "organisation/" + fixtures.OrgID} {
		eventually(t, ref+" is published", 20*time.Second, func() error {
			code, body, err := w.Parties.Status(w.ctx, http.MethodGet, "/v1/publications/"+ref, nil)
			if err != nil {
				return err
			}
			if code != http.StatusOK {
				return &httpErr{code, string(body)}
			}
			if !strings.Contains(string(body), `"face"`) {
				return &httpErr{code, "the publication carries no face: " + string(body)}
			}
			return nil
		})
	}

	v := w.verify(t, cred)
	if !v.Valid {
		t.Fatalf("the credential did not verify: %v", v.Reasons)
	}

	// The four links above the definition, by what they claim.
	want := map[string]bool{
		"instance-wide authorization":        false,
		"context grant":                      false,
		"is an organisation in the registry": false,
		"operates deployment":                false,
	}
	for _, l := range v.TrustChain {
		for phrase := range want {
			if strings.Contains(l.Claim, phrase) {
				want[phrase] = true
			}
		}
		// Every link answers the question the Link type exists to answer.
		if l.Checkable && l.How == "" {
			t.Errorf("a checkable link names nowhere to check: %+v", l)
		}
		if !l.Checkable && l.Trusting == "" {
			t.Errorf("an asserted link does not say what is being trusted: %+v", l)
		}
		// A link that says the verifier can check it must answer when they do.
		// Registry lookups only: the status-list link names the deployment's
		// in-network address, which a verifier resolves and this test host
		// does not, and it is proven by its own scenario.
		if l.Checkable && strings.Contains(l.How, "/dedi/lookup/") {
			resp, err := http.Get(l.How)
			if err != nil {
				t.Errorf("a checkable link's URL did not answer: %s: %v", l.How, err)
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("a checkable link's URL answered %d: %s", resp.StatusCode, l.How)
			}
		}
	}
	for phrase, seen := range want {
		if !seen {
			t.Errorf("the verdict's chain has no link for %q: %+v", phrase, v.TrustChain)
		}
	}

	// A complete chain leaves nothing about the authority undisclosed. What
	// stays not established is the worker's own authorization (#68) — that
	// is by design and is a different sentence.
	for _, s := range v.NotEstablished {
		if strings.Contains(s, "names no issuer authority") || strings.Contains(s, "the credential names none") {
			t.Errorf("a credential with a full chain reported it missing: %s", s)
		}
	}
	t.Logf("chain walked: %d links, %d not established", len(v.TrustChain), len(v.NotEstablished))
}

type httpErr struct {
	code int
	body string
}

func (e *httpErr) Error() string { return http.StatusText(e.code) + ": " + e.body }
