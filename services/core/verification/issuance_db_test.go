package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// This uses only the disposable database named by CREST_TEST_DATABASE_URL. It
// verifies the durable retry binding, including the metadata-only shape left
// after encrypted-wallet custody transfer.
func TestCredentialBindingSurvivesMetadataOnlyCustody(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schemaName := fmt.Sprintf("verification_binding_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, schemaName, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(context.Background(), "DROP SCHEMA \""+schemaName+"\" CASCADE") }()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	c := issuedCredential{
		ID: "credential-1", ClaimID: "claim-1", SubjectRef: "party-1",
		unitID: "unit-1", contextID: "project-1", route: "self",
		StatusIndex: 1, Digest: "digest-1", IssuedAt: time.Now().UTC(),
	}
	if err := db.InTx(ctx, func(tx store.Querier) error { return insertCredential(ctx, tx, c) }); err != nil {
		t.Fatal(err)
	}
	got, err := credentialByClaim(ctx, db.Q(), c.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	req := issueRequest{ClaimID: c.ClaimID, UnitID: c.unitID, PartyID: c.SubjectRef,
		ContextID: c.contextID, Route: c.route}
	if !credentialMatchesIssue(got, req) {
		t.Fatal("metadata-only credential did not retain its complete issuance binding")
	}
	for name, mutate := range map[string]func(*issueRequest){
		"unit":    func(r *issueRequest) { r.UnitID = "other-unit" },
		"party":   func(r *issueRequest) { r.PartyID = "other-party" },
		"context": func(r *issueRequest) { r.ContextID = "other-project" },
		"route":   func(r *issueRequest) { r.Route = "assisted" },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := req
			mutate(&wrong)
			if credentialMatchesIssue(got, wrong) {
				t.Fatalf("mismatched %s was accepted", name)
			}
		})
	}

	// A complete binding must be returned without touching evidence at all;
	// this is the outage case after custody transfer.
	var outageCalls int
	outage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		outageCalls++
		http.Error(w, "evidence unavailable", http.StatusBadGateway)
	}))
	defer outage.Close()
	h := &handlers{d: service.Deps{DB: db}, evidence: client.New(outage.URL)}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.issue(response, httptest.NewRequest(http.MethodPost, "/internal/credentials/issue", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("retry during evidence outage: HTTP %d: %s", response.Code, response.Body.String())
	}
	var returned issuedCredential
	if err := json.Unmarshal(response.Body.Bytes(), &returned); err != nil {
		t.Fatal(err)
	}
	if returned.ID != c.ID || returned.Digest != c.Digest || len(returned.Doc) != 0 {
		t.Fatal("retry changed the issued credential or restored its transferred document")
	}
	if outageCalls != 0 {
		t.Fatal("complete binding queried evidence during retry")
	}

	legacy := issuedCredential{
		ID: "credential-legacy", ClaimID: "claim-legacy", SubjectRef: "party-legacy",
		StatusIndex: 2, Digest: "digest-legacy", IssuedAt: time.Now().UTC(),
	}
	if err := db.InTx(ctx, func(tx store.Querier) error { return insertCredential(ctx, tx, legacy) }); err != nil {
		t.Fatal(err)
	}
	confirmedAt := time.Now().UTC()
	route := schema.ClaimConfirmationRouteSelf
	legacyClaim := schema.Claim{ID: legacy.ClaimID, UnitID: "unit-legacy", PartyID: legacy.SubjectRef,
		State:        schema.ClaimStateDISPUTED,
		Confirmation: &schema.ClaimConfirmation{At: &confirmedAt, Route: &route}}
	legacyUnit := schema.Unit{ID: "unit-legacy", ContextID: "project-legacy"}
	legacyWrong := issuedCredential{
		ID: "credential-legacy-wrong", ClaimID: "claim-legacy-wrong", SubjectRef: legacy.SubjectRef,
		unitID: "unit-tampered", route: string(route), StatusIndex: 3,
		Digest: "digest-legacy-wrong", IssuedAt: time.Now().UTC(),
	}
	if err := db.InTx(ctx, func(tx store.Querier) error { return insertCredential(ctx, tx, legacyWrong) }); err != nil {
		t.Fatal(err)
	}
	var legacyCalls int
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/claims/claim-legacy":
			_ = json.NewEncoder(w).Encode(legacyClaim)
		case "/internal/claims/claim-legacy-wrong":
			wrongClaim := legacyClaim
			wrongClaim.ID = legacyWrong.ClaimID
			_ = json.NewEncoder(w).Encode(wrongClaim)
		case "/internal/units/unit-legacy":
			_ = json.NewEncoder(w).Encode(legacyUnit)
		default:
			http.NotFound(w, r)
		}
	}))
	defer legacyServer.Close()
	legacyHandler := &handlers{d: service.Deps{DB: db}, evidence: client.New(legacyServer.URL)}
	legacyReq := issueRequest{ClaimID: legacy.ClaimID, UnitID: legacyUnit.ID,
		PartyID: legacy.SubjectRef, ContextID: legacyUnit.ContextID, Route: string(route)}
	if _, err := legacyHandler.existingCredentialForRetry(ctx, legacyReq); err != nil {
		t.Fatalf("legacy binding hydration failed: %v", err)
	}
	hydrated, err := credentialByClaim(ctx, db.Q(), legacy.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	if !hydrated.hasCompleteBinding() || !credentialMatchesIssue(hydrated, legacyReq) {
		t.Fatalf("legacy binding was not durably hydrated: %#v", hydrated)
	}
	if legacyCalls != 2 {
		t.Fatalf("legacy hydration calls = %d, want claim and unit", legacyCalls)
	}
	wrongReq := legacyReq
	wrongReq.ClaimID = legacyWrong.ClaimID
	if _, err := legacyHandler.existingCredentialForRetry(ctx, wrongReq); !errors.Is(err, errCredentialBindingMismatch) {
		t.Fatalf("known legacy binding mismatch error = %v, want mismatch", err)
	}
}
