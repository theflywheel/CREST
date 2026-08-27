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
	if err := w.Parties.As(w.login(t, fixtures.OrgID)).Post(
		w.ctx, "/v1/authorizations", auth, &created); err != nil {
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
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(
		w.ctx, "/v1/authorizations/overdue", &list); err != nil {
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
	if err := w.Parties.As(w.login(t, fixtures.OrgID)).Post(w.ctx,
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

// #124 review: a valid token is not authority to mint or revoke grants. The
// escalation it closes: a signed-in worker POSTs an ACTIVE authorization
// granting themselves act-for-party — naming themselves as approver and
// authority — and every later permits() check answers yes. Both doors, both
// halves: minting is refused, and revoking somebody else's grant by id is
// refused.
func TestAValidTokenIsNotAuthorityToMintOrRevokeGrants(t *testing.T) {
	w := setup(t)

	attacker := w.newWorker(t, "Self-Appointed Authority "+runID)
	epoch := w.w.Instance.Epoch
	end := epoch.Add(300 * 24 * time.Hour)
	mint := func(authority, approver string) (int, []byte) {
		t.Helper()
		code, body, err := w.Parties.As(w.login(t, attacker)).Status(w.ctx, http.MethodPost,
			"/v1/authorizations", schema.Authorization{
				PartyID: attacker,
				Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
				Scope: schema.AuthorizationScope{
					Kind:      schema.AuthorizationScopeKindContext,
					ContextID: ptr(fixtures.ProjectID),
				},
				Functions:         []string{"act-for-party"},
				Period:            schema.Period{Start: epoch, End: &end},
				AuthorityPartyID:  authority,
				ApprovedByPartyID: approver,
				ApprovedAt:        epoch,
				State:             schema.AuthorizationStateACTIVE,
			})
		if err != nil {
			t.Fatalf("mint attempt: %v", err)
		}
		return code, body
	}

	// Naming the organisation as authority without being it: impersonation.
	if code, body := mint(fixtures.OrgID, fixtures.OrgID); code != http.StatusForbidden {
		t.Fatalf("minting a grant in the organisation's name was answered %d, not 403: %s", code, body)
	}
	// Naming themselves as their own authority: a person is not an authority.
	if code, body := mint(attacker, attacker); code != http.StatusForbidden {
		t.Fatalf("a self-authorised grant was answered %d, not 403: %s", code, body)
	}

	// Bootstrapping an organisation-shaped party is not authority either: the
	// open party door plus a first bind must not add up to a grant mint. Only
	// the registry's APPROVED decision makes an organisation an authority.
	var fakeOrg schema.Party
	if err := w.Parties.Post(w.ctx, "/v1/parties", schema.Party{
		Kind:        schema.PartyKindOrganisation,
		DisplayName: "Bootstrapped Front " + runID,
		ContactRoutes: []schema.PartyContactRoutesItem{{
			Kind: schema.PartyContactRoutesItemKindEmail, Value: "front-" + runID + "@example.org",
		}},
	}, &fakeOrg); err != nil {
		t.Fatalf("bootstrap the front organisation: %v", err)
	}
	code, body, err := w.Parties.As(w.login(t, fakeOrg.ID)).Status(w.ctx, http.MethodPost,
		"/v1/authorizations", schema.Authorization{
			PartyID: attacker,
			Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
			Scope: schema.AuthorizationScope{
				Kind:      schema.AuthorizationScopeKindContext,
				ContextID: ptr(fixtures.ProjectID),
			},
			Functions:         []string{"act-for-party"},
			Period:            schema.Period{Start: epoch, End: &end},
			AuthorityPartyID:  fakeOrg.ID,
			ApprovedByPartyID: fakeOrg.ID,
			ApprovedAt:        epoch,
			State:             schema.AuthorizationStateACTIVE,
		})
	if err != nil {
		t.Fatalf("front-organisation mint attempt: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("a grant under a bootstrapped, never-approved organisation was answered %d, not 403: %s",
			code, body)
	}

	// And the revoke door: a grant the organisation stands behind cannot be
	// switched off by a stranger who learned its id.
	_, authID := w.grantSubmitter(t, "Grant Holder Under Attack "+runID, nil)
	code, body, err = w.Parties.As(w.login(t, attacker)).Status(w.ctx, http.MethodPost,
		"/v1/authorizations/"+url.PathEscape(authID)+"/revoke", nil)
	if err != nil {
		t.Fatalf("revoke attempt: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("a stranger revoking the organisation's grant was answered %d, not 403: %s", code, body)
	}

	// And the overwrite door: a rival organisation passes the authority gate
	// for itself, but reuses the first organisation's grant id. Grant ids are
	// public facts; letting this through would replace the original grant's
	// state and doc through another authority's gate.
	victimParty, victimID := w.grantSubmitter(t, "Grant To Overwrite "+runID, nil)
	rival := w.newOrganisation(t, "Rival Authority "+runID)
	code, body, err = w.Parties.As(w.login(t, rival)).Status(w.ctx, http.MethodPost,
		"/v1/authorizations", schema.Authorization{
			ID:      victimID,
			PartyID: rival,
			Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
			Scope: schema.AuthorizationScope{
				Kind:      schema.AuthorizationScopeKindContext,
				ContextID: ptr(fixtures.ProjectID),
			},
			Functions:         []string{"submit-evidence"},
			Period:            schema.Period{Start: epoch, End: &end},
			AuthorityPartyID:  rival,
			ApprovedByPartyID: rival,
			ApprovedAt:        epoch,
			State:             schema.AuthorizationStateREVOKED,
		})
	if err != nil {
		t.Fatalf("overwrite attempt: %v", err)
	}
	if code != http.StatusConflict {
		t.Fatalf("reusing another authority's grant id was answered %d, not 409: %s", code, body)
	}
	// The original grant still permits what it permitted: the refusal
	// protected the row, not just the response code.
	var permitted struct {
		Permitted bool `json:"permitted"`
	}
	if err := w.Parties.As(w.login(t, fixtures.CustodianID)).Get(w.ctx, fmt.Sprintf(
		"/v1/authorizations/permits?partyId=%s&function=submit-work-evidence&contextId=%s",
		url.QueryEscape(victimParty), fixtures.ProjectID), &permitted); err != nil {
		t.Fatalf("read permits after the attack: %v", err)
	}
	if !permitted.Permitted {
		t.Fatal("the overwrite attempt was refused but the original grant no longer permits")
	}

	// And the edit door: the SAME authority reusing the id with different
	// content. A grant is narrowed by revoke and re-grant, never edited in
	// place — an in-place rewrite would update the stored document while the
	// indexed permission columns keep the old row, a grant that says one thing
	// while permits() enforces another.
	victimDoc := schema.Authorization{
		ID:      victimID,
		PartyID: victimParty,
		Terms:   schema.VersionedRef{ID: fixtures.TermsID, Version: 1},
		Scope: schema.AuthorizationScope{
			Kind:      schema.AuthorizationScopeKindContext,
			ContextID: ptr(fixtures.ProjectID),
		},
		Functions:         []string{"submit-work-evidence"},
		Period:            schema.Period{Start: epoch.Add(-30 * 24 * time.Hour), End: &end},
		AuthorityPartyID:  fixtures.OrgID,
		ApprovedByPartyID: fixtures.OrgID,
		ApprovedAt:        epoch.Add(-30 * 24 * time.Hour),
		State:             schema.AuthorizationStateACTIVE,
	}
	edited := victimDoc
	edited.Functions = []string{"submit-work-evidence", "act-for-party"}
	code, body, err = w.Parties.As(w.login(t, fixtures.OrgID)).Status(w.ctx, http.MethodPost,
		"/v1/authorizations", edited)
	if err != nil {
		t.Fatalf("edit attempt: %v", err)
	}
	if code != http.StatusConflict {
		t.Fatalf("editing a grant in place through create was answered %d, not 409: %s", code, body)
	}

	// An exact replay of the same document is still accepted — that is what
	// lets a reseed load fixture history twice without failing.
	code, body, err = w.Parties.As(w.login(t, fixtures.OrgID)).Status(w.ctx, http.MethodPost,
		"/v1/authorizations", victimDoc)
	if err != nil {
		t.Fatalf("replay attempt: %v", err)
	}
	if code != http.StatusCreated {
		t.Fatalf("an exact replay of an existing grant was answered %d, not 201: %s", code, body)
	}
}

// A grant read by id names a subject and their functions — for a person, a
// record of who works where (#68). Only the grant's own authority may read
// it; a valid token held by anyone else is not enough. The seeder leans on
// this read to compare a standing grant's shape against the fixture instead
// of trusting the permits predicate, which cannot see scope drift.
func TestAGrantIsReadableByItsAuthorityAlone(t *testing.T) {
	w := setup(t)
	holder, authID := w.grantSubmitter(t, "Grant Read Holder "+runID, nil)

	var standing schema.Authorization
	if err := w.Parties.As(w.login(t, fixtures.OrgID)).
		Get(w.ctx, "/v1/authorizations/"+url.PathEscape(authID), &standing); err != nil {
		t.Fatalf("the authority cannot read its own grant: %v", err)
	}
	if standing.ID != authID || standing.PartyID != holder {
		t.Fatalf("read grant %q for %q, want %q for %q",
			standing.ID, standing.PartyID, authID, holder)
	}

	stranger := w.newWorker(t, "Grant Reader "+runID)
	code, _, err := w.Parties.As(w.login(t, stranger)).Status(w.ctx,
		http.MethodGet, "/v1/authorizations/"+url.PathEscape(authID), nil)
	if err != nil {
		t.Fatalf("stranger read: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("a stranger reading a grant answered %d, want 403", code)
	}
}

// Issuance is the substrate's act (#127, #137). The confirmation service —
// the payments application — no longer holds keys, a credential store or a
// status list: it asks verification to issue at a confirming exit, and every
// credential question is answered on the infrastructure side of the boundary.
// The positive half (an exit still lands a signed credential in the wallet)
// is the spine's assertion; this is the negative half — the application
// really did give the surface up.
func TestIssuanceLivesInTheSubstrateNotThePaymentsApplication(t *testing.T) {
	w := setup(t)
	for _, path := range []string{"/v1/issuer", "/v1/status-list", "/v1/credentials?partyId=x"} {
		code, _, err := w.Confirmation.Status(w.ctx, http.MethodGet, path, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if code != http.StatusNotFound {
			t.Fatalf("confirmation still answers %s (%d); issuance belongs to the substrate", path, code)
		}
	}
	var issuer struct {
		Issuer string `json:"issuer"`
		Key    string `json:"publicKeyMultibase"`
	}
	if err := w.Verification.Get(w.ctx, "/v1/issuer", &issuer); err != nil {
		t.Fatalf("verification does not publish the issuer key: %v", err)
	}
	if issuer.Issuer == "" || issuer.Key == "" {
		t.Fatalf("verification's issuer info is incomplete: %+v", issuer)
	}
}

// The one atomicity gap the issuance extraction opened, closed (#137, dough's
// review of #143): a confirming exit can crash after verification committed
// the credential and before the window recorded the exit. If the worker then
// disputes the still-open window, the dispute must not leave that orphan
// standing — a credential for a disputed claim is a false statement CREST
// signed. This simulates the crash's leftover by issuing directly, then
// disputes, and expects a dispute exit with no credential and the orphan
// revoked. Distinct from a dispute after auto-confirm, where the credential
// was legitimately issued and deliberately stands (W3).
func TestADisputeRevokesACrashOrphanedCredential(t *testing.T) {
	w := setup(t)
	phone := sharedNumber(209)
	worker := newWorkerWithPhone(t, w, "Orphan Dispute", phone)
	result := w.submit(t, batch(row(phone, 2, "HH-ORPHAN")))
	claimID := result.ClaimIDs[0]
	// The window opens through evidence's outbox, not synchronously with the
	// submission.
	var win winView
	eventually(t, "the window opens", 15*time.Second, func() error {
		v, err := w.window(claimID)
		if err != nil {
			return err
		}
		win = v
		return nil
	})

	// The crash's leftover: issuance committed, exit never recorded.
	var orphan struct {
		ID string `json:"id"`
	}
	if err := w.Verification.Post(w.ctx, "/internal/credentials/issue", map[string]any{
		"claimId": claimID, "unitId": win.UnitID, "partyId": worker,
		"route": "self", "at": win.OpenedAt,
	}, &orphan); err != nil {
		t.Fatalf("plant the orphan: %v", err)
	}

	if err := w.disputeClaim(t, claimID, map[string]any{
		"reason": "that visit never happened", "raisedByPartyId": worker,
	}, nil); err != nil {
		t.Fatalf("dispute: %v", err)
	}

	after, err := w.window(claimID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ExitRoute == nil || *after.ExitRoute != "dispute" {
		t.Fatalf("window exit is %v, want dispute", after.ExitRoute)
	}
	if after.CredentialID != nil {
		t.Fatalf("a dispute exit recorded credential %s", *after.CredentialID)
	}
	if after.PaymentReleasedAt == nil {
		t.Fatalf("the dispute exit released no payment (every exit releases payment)")
	}
	var got struct {
		RevokedAt *time.Time `json:"revokedAt"`
	}
	if err := w.Verification.Get(w.ctx,
		"/internal/credentials/by-claim/"+claimID, &got); err != nil {
		t.Fatalf("read the orphan back: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatalf("the orphaned credential still stands after the dispute")
	}
}
