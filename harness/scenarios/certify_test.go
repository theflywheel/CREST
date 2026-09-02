//go:build e2e

package scenarios

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// The Certify data surface (#155 phase C): what Inji Certify's data-provider
// plugin reads at issuance time, in place of the CSV fixture finding C16
// documented. The contract under test is the one the WorkEventCredential
// template references — swap the plugin's source and the credential must not
// change shape.

type certifyEventView struct {
	UnitID            string  `json:"unitId"`
	ClaimID           string  `json:"claimId"`
	Activity          string  `json:"activity"`
	DefinitionRef     string  `json:"definitionRef"`
	DefinitionVersion string  `json:"definitionVersion"`
	PeriodStart       string  `json:"periodStart"`
	OutcomeValue      float64 `json:"outcomeValue"`
	OutcomeUnit       string  `json:"outcomeUnit"`
	ContextRef        string  `json:"contextRef"`
	SourceClass       string  `json:"sourceClass"`
	CaptureMethod     string  `json:"captureMethod"`
	AdapterRef        string  `json:"adapterRef"`
	ReceivedAt        string  `json:"receivedAt"`
	SourceExposure    string  `json:"sourceExposure"`
}

func (w *world) certifyEvents(t *testing.T, providerSub string) (json.RawMessage, []certifyEventView) {
	t.Helper()
	var raw json.RawMessage
	path := fmt.Sprintf("/internal/certify/work-events?issuer=%s&subject=%s",
		url.QueryEscape(w.oidc.Issuer), url.QueryEscape(providerSub))
	if err := w.Verification.Get(w.ctx, path, &raw); err != nil {
		t.Fatalf("read certify work events: %v", err)
	}
	var out struct {
		WorkEvents []certifyEventView `json:"workEvents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse certify work events: %v", err)
	}
	return raw, out.WorkEvents
}

func TestCertifyReadsTheConfirmedFactsNotAFixture(t *testing.T) {
	w := setup(t)
	claimID, credID := w.issueThenDispute(t, "certify-live")
	if credID == "" {
		t.Fatal("no credential issued")
	}

	// The raw provider subject, exactly as the wallet's access token would
	// carry it — the pairwise derivation is the endpoint's job, not the
	// caller's. login() bound this subject when the claim was confirmed.
	providerSub := fmt.Sprintf("%s|%s", runID,
		strings.TrimPrefix(w.windowParty(t, claimID), "did:crest:party:"))
	// Logged so a person driving the local wallet proof (make certify-issue
	// against this stack) can mint the same caller and act for this worker.
	t.Logf("worker provider sub: %s", providerSub)

	raw, events := w.certifyEvents(t, providerSub)
	if len(events) == 0 {
		t.Fatal("a confirmed, credentialed claim served no work events")
	}
	var got *certifyEventView
	for i := range events {
		if events[i].ClaimID == claimID {
			got = &events[i]
		}
	}
	if got == nil {
		t.Fatalf("the confirmed claim %s is not among the served events", claimID)
	}
	if got.Activity != "bednet-distribution" || got.OutcomeValue != 9 ||
		got.OutcomeUnit != "bednets-distributed" {
		t.Fatalf("the facts do not match the confirmed work: %+v", got)
	}
	for name, v := range map[string]string{
		"unitId": got.UnitID, "definitionRef": got.DefinitionRef,
		"definitionVersion": got.DefinitionVersion, "periodStart": got.PeriodStart,
		"contextRef": got.ContextRef, "sourceClass": got.SourceClass,
		"captureMethod": got.CaptureMethod, "adapterRef": got.AdapterRef,
		"receivedAt": got.ReceivedAt, "sourceExposure": got.SourceExposure,
	} {
		if v == "" {
			t.Errorf("%s is empty; the template renders it into the credential", name)
		}
	}
	// W9 and §6 on the wire: nothing identifying, and no stored judgement.
	lower := strings.ToLower(string(raw))
	for _, banned := range []string{"individualid", "nationalid", "\"tier\"", "phone", "\"name\""} {
		if strings.Contains(lower, banned) {
			t.Errorf("the certify surface leaks %q: %s", banned, raw)
		}
	}
}

func TestAStrangerGetsAnEmptyListNotAnError(t *testing.T) {
	w := setup(t)
	// Authenticated-but-unenrolled is a person, not a fault: Certify turns
	// the empty list into its own "no data" refusal.
	_, events := w.certifyEvents(t, "nobody-ever-bound-"+runID)
	if len(events) != 0 {
		t.Fatalf("an unbound subject served %d events", len(events))
	}
}

func (w *world) windowParty(t *testing.T, claimID string) string {
	t.Helper()
	win, err := w.window(claimID)
	if err != nil {
		t.Fatalf("read window: %v", err)
	}
	return win.PartyID
}
