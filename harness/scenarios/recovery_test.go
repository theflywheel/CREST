//go:build e2e

package scenarios

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// Recovery (§16, #106): a lost handset must not cost a worker their history —
// and the one shortcut through that rule can never be quiet.

type recoveryView struct {
	ID            string  `json:"id"`
	State         string  `json:"state"`
	OverrideBy    *string `json:"overrideByPartyId"`
	OverrideRsn   *string `json:"overrideReason"`
	ReviewBy      *string `json:"reviewBy"`
	Confirmations []struct {
		ConfirmerPartyID string `json:"confirmerPartyId"`
		AuthorityPartyID string `json:"authorityPartyId"`
	} `json:"confirmations"`
}

// vouchedParty creates a person holding an ACTIVE authorization from the given
// authority, which is what makes them an eligible recovery confirmer.
func (w *world) vouchedParty(t *testing.T, name, authority, contextID string, authorityCaller harness.Caller) string {
	t.Helper()
	party := w.newWorker(t, name)
	epoch := w.w.Instance.Epoch
	end := epoch.Add(300 * 24 * time.Hour)
	// As the authority itself: minting a grant is the named authority saying
	// so, and the registry now refuses a caller who is not the authority the
	// grant names (#124 review).
	if err := w.Parties.As(authorityCaller).Post(
		w.ctx, "/v1/authorizations", schema.Authorization{
			PartyID:           party,
			Terms:             schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
			Scope:             schema.AuthorizationScope{Kind: schema.AuthorizationScopeKindContext, ContextID: ptr(contextID)},
			Functions:         []string{"submit-work-evidence"},
			Period:            schema.Period{Start: epoch.Add(-30 * 24 * time.Hour), End: &end},
			AuthorityPartyID:  authority,
			ApprovedByPartyID: authority,
			ApprovedAt:        epoch.Add(-30 * 24 * time.Hour),
			State:             schema.AuthorizationStateACTIVE,
		}, nil); err != nil {
		t.Fatalf("vouch for %s: %v", name, err)
	}
	return party
}

// newOrganisation creates a second authority, because the anti-stacking rule
// is about authorities and a fixture with one org cannot exercise it.
func (w *world) newOrganisation(t *testing.T, name string) string {
	t.Helper()
	// The full onboarding, not a bare party: an organisation-shaped party is
	// not an authority until the registry's decision says so, and the grant
	// gate reads the registration. A helper that skipped the decision would
	// hand scenarios an authority the real system refuses.
	applicant := w.registrationApplicant(t, name)
	var out struct {
		Party schema.Party `json:"party"`
	}
	if err := w.Parties.As(applicant).Post(w.ctx, "/v1/organisations", schema.Party{
		Kind:        schema.PartyKindOrganisation,
		DisplayName: name,
		ContactRoutes: []schema.PartyContactRoutesItem{{
			Kind: schema.PartyContactRoutesItemKindEmail, Value: "org-" + runID + "@example.org",
		}},
	}, &out); err != nil {
		t.Fatalf("register organisation: %v", err)
	}
	orgID := out.Party.ID
	terms := w.w.Terms[0]
	if err := w.Parties.As(applicant).Post(w.ctx, "/v1/organisations/"+orgID+"/terms-acceptance",
		map[string]any{"termsId": terms.ID, "termsVersion": terms.Version, "acceptedBy": orgID},
		nil); err != nil {
		t.Fatalf("accept terms: %v", err)
	}
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Post(w.ctx,
		"/v1/organisations/"+orgID+"/decision",
		map[string]any{"approve": true, "decidedBy": fixtures.CustodianID}, nil); err != nil {
		t.Fatalf("approve organisation: %v", err)
	}
	return orgID
}

func TestTwoVoicesFromDistinctAuthoritiesRecoverAWorker(t *testing.T) {
	w := setup(t)

	worker := w.newWorker(t, "Lost Handset "+runID)
	orgBName := "Second Authority " + runID
	orgB := w.newOrganisation(t, orgBName)
	orgBCaller := w.registrationApplicant(t, orgBName)
	var secondProject struct {
		ID string `json:"id"`
	}
	if err := w.Parties.As(orgBCaller).Post(w.ctx, "/v1/projects", map[string]any{
		"kind": "project", "name": "Second authority project " + runID, "ownerPartyId": orgB,
	}, &secondProject); err != nil {
		t.Fatalf("create second authority project: %v", err)
	}
	if err := w.Parties.As(orgBCaller).Post(w.ctx,
		"/v1/projects/"+secondProject.ID+"/activation", nil, nil); err != nil {
		t.Fatalf("activate second authority project: %v", err)
	}
	confirmerA := w.vouchedParty(t, "Confirmer A "+runID, fixtures.OrgID, fixtures.ProjectID, w.login(t, fixtures.OrgID))
	confirmerA2 := w.vouchedParty(t, "Confirmer A2 "+runID, fixtures.OrgID, fixtures.ProjectID, w.login(t, fixtures.OrgID))
	confirmerB := w.vouchedParty(t, "Confirmer B "+runID, orgB, secondProject.ID, orgBCaller)

	var rec recoveryView
	if err := w.Parties.As(w.login(t, fixtures.SupervisorID)).Post(w.ctx, "/v1/recoveries", map[string]any{
		"partyId": worker, "openedByPartyId": fixtures.SupervisorID,
		"reason": "handset lost in the river crossing",
	}, &rec); err != nil {
		t.Fatalf("open recovery: %v", err)
	}

	// Nobody vouches for themselves — shown on a second recovery whose subject
	// CAN authenticate, because the main worker must stay unbound: logging
	// them in would bind an anchor and quietly raise the assurance this test
	// asserts at the end.
	var recA recoveryView
	if err := w.Parties.As(w.login(t, fixtures.SupervisorID)).Post(w.ctx, "/v1/recoveries", map[string]any{
		"partyId": confirmerA, "openedByPartyId": fixtures.SupervisorID,
		"reason": "self-confirmation probe",
	}, &recA); err != nil {
		t.Fatalf("open probe recovery: %v", err)
	}
	code, body, _ := w.Parties.As(w.login(t, confirmerA)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+recA.ID+"/confirmations",
		map[string]any{"confirmerPartyId": confirmerA, "authorityPartyId": fixtures.OrgID})
	if code != http.StatusBadRequest {
		t.Fatalf("self-confirmation was answered %d, not 400: %s", code, body)
	}

	// A confirmer the declared authority does not stand behind is refused.
	code, body, _ = w.Parties.As(w.login(t, confirmerA)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+rec.ID+"/confirmations",
		map[string]any{"confirmerPartyId": confirmerA, "authorityPartyId": orgB})
	if code != http.StatusForbidden {
		t.Fatalf("an unvouched confirmer was answered %d, not 403: %s", code, body)
	}

	// First voice.
	if err := w.Parties.As(w.login(t, confirmerA)).Post(w.ctx, "/v1/recoveries/"+rec.ID+"/confirmations",
		map[string]any{"confirmerPartyId": confirmerA, "authorityPartyId": fixtures.OrgID}, &rec); err != nil {
		t.Fatalf("first confirmation: %v", err)
	}
	if rec.State != "OPEN" {
		t.Fatalf("one voice decided a recovery: state %s", rec.State)
	}

	// A second voice from the SAME authority does not count — one org, one
	// voice, enforced by the table itself.
	code, body, _ = w.Parties.As(w.login(t, confirmerA2)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+rec.ID+"/confirmations",
		map[string]any{"confirmerPartyId": confirmerA2, "authorityPartyId": fixtures.OrgID})
	if code != http.StatusConflict {
		t.Fatalf("a second same-authority voice was answered %d, not 409: %s\n"+
			"Three confirmers appointed by one organisation is that organisation's "+
			"decision wearing three names.", code, body)
	}

	// A voice from a different authority completes the panel.
	if err := w.Parties.As(w.login(t, confirmerB)).Post(w.ctx, "/v1/recoveries/"+rec.ID+"/confirmations",
		map[string]any{"confirmerPartyId": confirmerB, "authorityPartyId": orgB}, &rec); err != nil {
		t.Fatalf("second confirmation: %v", err)
	}
	if rec.State != "CONFIRMED" {
		t.Fatalf("two distinct voices did not decide the recovery: state %s", rec.State)
	}

	// Completion appends a binding for the worker's new subject; the worker
	// can authenticate again, and their assurance says honestly what happened.
	newSubject := w.oidc.Subject(runID + "|recovered-worker")
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Post(w.ctx, "/v1/recoveries/"+rec.ID+"/complete",
		map[string]any{"subjectRef": newSubject}, &rec); err != nil {
		t.Fatalf("complete recovery: %v", err)
	}

	var bound struct {
		PartyID string `json:"partyId"`
	}
	if err := w.Parties.Get(w.ctx,
		"/internal/identity/subjects/"+newSubject, &bound); err != nil {
		t.Fatalf("the recovered subject does not resolve: %v", err)
	}
	if bound.PartyID != worker {
		t.Fatalf("the recovered subject resolves to %s, not the worker", bound.PartyID)
	}

	// Vouching is community knowledge, not a national identity check: IA-1
	// until the worker re-anchors, at which point the stronger binding wins.
	var assurance struct {
		Level string `json:"identityAssurance"`
	}
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, "/internal/parties/"+worker+"/assurance", &assurance); err != nil {
		t.Fatalf("read assurance: %v", err)
	}
	if assurance.Level != "IA-1" {
		t.Fatalf("a recovered-but-unanchored worker reads as %q, want IA-1", assurance.Level)
	}
}

func TestAnOverrideWithoutAReasonCannotBeExpressed(t *testing.T) {
	w := setup(t)

	worker := w.newWorker(t, "Remote Worker "+runID)
	var rec recoveryView
	if err := w.Parties.As(w.login(t, fixtures.SupervisorID)).Post(w.ctx, "/v1/recoveries", map[string]any{
		"partyId": worker, "openedByPartyId": fixtures.SupervisorID,
		"reason": "no second confirmer within a day's travel",
	}, &rec); err != nil {
		t.Fatalf("open recovery: %v", err)
	}

	// No reason, no override — the refusal this whole path is built around.
	code, body, _ := w.Parties.As(w.login(t, fixtures.SupervisorID)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+rec.ID+"/override",
		map[string]any{"byPartyId": fixtures.SupervisorID})
	if code != http.StatusBadRequest {
		t.Fatalf("an override with no reason was answered %d, not 400: %s", code, body)
	}
	if !strings.Contains(string(body), "override_needs_a_reason_and_an_owner") {
		t.Fatalf("the refusal does not name the rule: %s", body)
	}

	// Nobody without the function overrides, reason or not.
	code, body, _ = w.Parties.As(w.login(t, fixtures.SupervisorID)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+rec.ID+"/override",
		map[string]any{"byPartyId": fixtures.SupervisorID, "reason": "confirmers unreachable"})
	if code != http.StatusForbidden {
		t.Fatalf("an override without the override-recovery function was answered %d, not 403: %s", code, body)
	}

	// Grant the function to the OPERATOR — the custodian here, standing in for
	// the deployment operator. Instance-scoped, because the permits check for
	// an override deliberately matches no context grant: this is an
	// operator-level power (G1, #10).
	epoch := w.w.Instance.Epoch
	end := epoch.Add(300 * 24 * time.Hour)
	grantOverride := func(party string) {
		if err := w.Parties.As(w.login(t, fixtures.OrgID)).Post(
			w.ctx, "/v1/authorizations", schema.Authorization{
				PartyID:           party,
				Terms:             schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
				Scope:             schema.AuthorizationScope{Kind: schema.AuthorizationScopeKindInstance},
				Functions:         []string{"override-recovery"},
				Period:            schema.Period{Start: epoch.Add(-30 * 24 * time.Hour), End: &end},
				AuthorityPartyID:  fixtures.OrgID,
				ApprovedByPartyID: fixtures.OrgID,
				ApprovedAt:        epoch.Add(-30 * 24 * time.Hour),
				State:             schema.AuthorizationStateACTIVE,
			}, nil); err != nil {
			t.Fatalf("grant override-recovery to %s: %v", party, err)
		}
	}

	// G1's line: the worker's OWN supervisor is refused even holding the
	// function, because the person who caused a lockout is frequently the
	// supervisor, and a path they can invoke is one they can invoke against
	// the worker. newWorker names the fixture supervisor as the worker's
	// supervisor route, which is exactly the relationship being tested.
	grantOverride(fixtures.SupervisorID)
	code, body, _ = w.Parties.As(w.login(t, fixtures.SupervisorID)).Status(w.ctx, http.MethodPost,
		"/v1/recoveries/"+rec.ID+"/override",
		map[string]any{"byPartyId": fixtures.SupervisorID, "reason": "confirmers unreachable"})
	if code != http.StatusForbidden || !strings.Contains(string(body), "own_supervisor_cannot_override") {
		t.Fatalf("the worker's own supervisor overrode their recovery (%d): %s", code, body)
	}

	grantOverride(fixtures.CustodianID)
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Post(w.ctx, "/v1/recoveries/"+rec.ID+"/override", map[string]any{
		"byPartyId": fixtures.CustodianID, "reason": "confirmers unreachable; verified in person",
	}, &rec); err != nil {
		t.Fatalf("override: %v", err)
	}
	if rec.State != "OVERRIDDEN" || rec.OverrideBy == nil || rec.OverrideRsn == nil || rec.ReviewBy == nil {
		t.Fatalf("the override record is incomplete: %+v", rec)
	}

	// Completion binds the new subject, exactly as the 2-of-3 path does.
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Post(w.ctx, "/v1/recoveries/"+rec.ID+"/complete",
		map[string]any{"subjectRef": w.oidc.Subject(runID + "|overridden-worker")}, &rec); err != nil {
		t.Fatalf("complete after override: %v", err)
	}

	// The override is readable afterwards — by the worker, by an auditor, by
	// anybody asking whether this power is being used well.
	var read recoveryView
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, "/v1/recoveries/"+rec.ID, &read); err != nil {
		t.Fatalf("read the recovery back: %v", err)
	}
	if read.OverrideBy == nil || read.OverrideRsn == nil {
		t.Fatalf("the override is not readable afterwards: %+v", read)
	}

	// Flagged for review, never silent: past the review date it surfaces.
	if err := w.Advance(w.ctx, 91*24*time.Hour); err != nil {
		t.Fatalf("advance the clock: %v", err)
	}
	var overdue struct {
		Recoveries []recoveryView `json:"recoveries"`
	}
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, "/v1/recoveries?overdue=true", &overdue); err != nil {
		t.Fatalf("list overdue overrides: %v", err)
	}
	found := false
	for _, r := range overdue.Recoveries {
		if r.ID == rec.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("an override past its review date is not on the review list; "+
			"\"flagged for review\" only holds if somebody can find the flag (%d listed)",
			len(overdue.Recoveries))
	}
}
