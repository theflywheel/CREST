package verification

import (
	"reflect"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

func TestValidateIssuanceRejectsForgedClaimInputs(t *testing.T) {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	claim := schema.Claim{
		ID: "claim-1", UnitID: "unit-1", PartyID: "party-1", State: schema.ClaimStateACCEPTED,
		Confirmation: &schema.ClaimConfirmation{At: &at, Route: routePtr(schema.ClaimConfirmationRouteSelf)},
	}
	unit := schema.Unit{ID: "unit-1", ContextID: "project-1"}
	req := issueRequest{ClaimID: "claim-1", UnitID: "unit-1", PartyID: "party-1", ContextID: "project-1", Route: "self"}
	if err := validateIssuance(req, claim, unit); err != nil {
		t.Fatalf("valid issuance rejected: %v", err)
	}
	for name, mutate := range map[string]func(*issueRequest, *schema.Claim, *schema.Unit){
		"wrong party":   func(r *issueRequest, _ *schema.Claim, _ *schema.Unit) { r.PartyID = "other" },
		"wrong unit":    func(r *issueRequest, _ *schema.Claim, _ *schema.Unit) { r.UnitID = "other" },
		"wrong context": func(_ *issueRequest, _ *schema.Claim, u *schema.Unit) { u.ContextID = "other" },
		"wrong route":   func(r *issueRequest, _ *schema.Claim, _ *schema.Unit) { r.Route = "assisted" },
		"unaccepted":    func(_ *issueRequest, c *schema.Claim, _ *schema.Unit) { c.State = schema.ClaimStateDISPUTED },
	} {
		r, c, u := req, claim, unit
		mutate(&r, &c, &u)
		if err := validateIssuance(r, c, u); err == nil {
			t.Errorf("%s: expected claim binding rejection, got %v", name, err)
		}
	}
}

func TestEvidenceFieldsOfUsesCanonicalPresenceVocabulary(t *testing.T) {
	end := time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC)
	sourceRecordRef := "source-record-1"
	emptyGeography := ""
	unit := schema.Unit{
		Outcome:   schema.Outcome{Unit: "bednets", Value: 0},
		Period:    schema.Period{Start: end, End: &end},
		Geography: &emptyGeography,
		Enrichment: map[string]any{
			"beneficiaryCount": 0,
			"empty":            "",
		},
		Provenance: schema.Provenance{SourceRecordRef: &sourceRecordRef},
	}
	want := []string{
		"activity", "beneficiaryCount", "outcome_unit", "outcome_value",
		"period_end", "period_start", "source_record_ref",
	}
	if got := evidenceFieldsOf(unit); !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence fields = %#v, want %#v", got, want)
	}
	persisted := []string{"worker_id_kind", "activity", "worker_id", "period_start"}
	// The sidecar is sorted and deduplicated before it enters a credential.
	wantPersisted := []string{"activity", "period_start", "worker_id", "worker_id_kind"}
	if got := evidenceFieldsOrFallback(unit, persisted); !reflect.DeepEqual(got, wantPersisted) {
		t.Fatalf("persisted evidence fields = %#v, want %#v", got, wantPersisted)
	}
}

func TestCredentialMatchesIssuePreservesBindingAfterCustodyTransfer(t *testing.T) {
	req := issueRequest{ClaimID: "claim-1", UnitID: "unit-1", PartyID: "party-1", Route: "self"}
	metadataOnly := issuedCredential{ClaimID: req.ClaimID, SubjectRef: req.PartyID,
		unitID: req.UnitID, contextID: "project-1", route: req.Route}
	req.ContextID = "project-1"
	if !credentialMatchesIssue(metadataOnly, req) {
		t.Fatal("metadata-only credential should be idempotently returned after custody transfer")
	}
	if credentialMatchesIssue(metadataOnly, issueRequest{ClaimID: req.ClaimID, UnitID: req.UnitID, PartyID: "other", Route: req.Route}) {
		t.Fatal("credential retry must not cross the subject binding")
	}
	if credentialMatchesIssue(metadataOnly, issueRequest{ClaimID: req.ClaimID, UnitID: req.UnitID,
		PartyID: req.PartyID, ContextID: "other-project", Route: req.Route}) {
		t.Fatal("credential retry must not cross the context binding")
	}
	if credentialMatchesIssue(metadataOnly, issueRequest{ClaimID: req.ClaimID, UnitID: req.UnitID,
		PartyID: req.PartyID, ContextID: req.ContextID, Route: "assisted"}) {
		t.Fatal("credential retry must not cross the confirmation route binding")
	}

	retained := metadataOnly
	retained.Doc = []byte(`{"credentialSubject":{"id":"party-1","confirmation":{"route":"self"},"workEvent":{"claimId":"claim-1","eventId":"unit-1"}}}`)
	if !credentialMatchesIssue(retained, req) {
		t.Fatal("retained credential should match its original request")
	}
	if credentialMatchesIssue(retained, issueRequest{ClaimID: req.ClaimID, UnitID: "other", PartyID: req.PartyID, Route: req.Route}) {
		t.Fatal("retained credential must not match a different unit")
	}
}

func routePtr(v schema.ClaimConfirmationRoute) *schema.ClaimConfirmationRoute { return &v }
