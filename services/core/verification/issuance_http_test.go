package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func TestHTTPIssuanceUsesAuthoritativeConfirmationAndStoredEvidenceFields(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.Open(ctx, dsn, "verification_http_"+time.Now().Format("20060102150405.000000000"), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := db.Schema()
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(context.Background(), `DROP SCHEMA "`+schemaName+`" CASCADE`) }()
	if err := db.Migrate(ctx, migrations, "migrations"); err != nil {
		t.Fatal(err)
	}

	claimAt := time.Date(2026, 9, 6, 14, 30, 0, 0, time.UTC)
	requestAt := claimAt.Add(-48 * time.Hour)
	route := schema.ClaimConfirmationRouteSelf
	partyID := "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAV"
	claimID := "crest:claim:01ARZ3NDEKTSV4RRFFQ69G5FAW"
	unitID := "crest:unit:01ARZ3NDEKTSV4RRFFQ69G5FAX"
	definitionID := "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAY"
	contextID := "crest:context:01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	sourceRef := "csv-row-42"
	systemRef := "source:csv:test"
	fields := []string{"activity", "outcome_unit", "outcome_value", "period_start", "source_record_ref", "worker_id", "worker_id_kind"}
	unit := schema.Unit{
		ID: unitID, ContextID: contextID,
		Definition: schema.VersionedRef{ID: definitionID, Version: 3},
		Outcome:    schema.Outcome{Value: 1, Unit: "visits"},
		Period:     schema.Period{Start: claimAt.Add(-time.Hour)},
		Provenance: schema.Provenance{
			AdapterRef: "csv@1", CaptureMethod: schema.CaptureMethodSystemOfRecord,
			ReceivedAt: claimAt.Add(-2 * time.Hour), SourceClass: schema.SourceClassInstitutionalSystem,
			SourceExposure: schema.SourceExposureSignedBatch, SourceRecordRef: &sourceRef, SystemRef: &systemRef,
		},
	}
	claim := schema.Claim{
		ID: claimID, UnitID: unitID, PartyID: partyID, State: schema.ClaimStateACCEPTED,
		Confirmation: &schema.ClaimConfirmation{At: &claimAt, Route: &route},
	}

	evidence := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/claims/" + claimID:
			_ = json.NewEncoder(w).Encode(claim)
		case "/internal/units/" + unitID:
			_ = json.NewEncoder(w).Encode(struct {
				schema.Unit
				EvidenceFields []string `json:"evidenceFields,omitempty"`
			}{Unit: unit, EvidenceFields: fields})
		default:
			http.NotFound(w, r)
		}
	}))
	defer evidence.Close()
	optional := httptest.NewServer(http.NotFoundHandler())
	defer optional.Close()
	issuer, err := credential.NewIssuer("did:crest:issuer:test", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	h := &handlers{
		d:        service.Deps{DB: db, Clock: clock.NewFake(claimAt), Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		evidence: evidenceClient(evidence.URL), definitions: client.New(optional.URL), registry: client.New(optional.URL),
		issuer: issuer, statusListURL: "https://verification.example/status-list",
	}
	body, err := json.Marshal(issueRequest{ClaimID: claimID, UnitID: unitID, PartyID: partyID,
		ContextID: contextID, Route: string(route), At: requestAt})
	if err != nil {
		t.Fatal(err)
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/credentials/issue", bytes.NewReader(body))
	h.issue(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("issue returned HTTP %d: %s", resp.Code, resp.Body.String())
	}
	var issued issuedCredential
	if err := json.Unmarshal(resp.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !issued.IssuedAt.Equal(claimAt) {
		t.Fatalf("issuedAt = %s, want authoritative claim time %s", issued.IssuedAt, claimAt)
	}
	if issued.IssuedAt.Equal(requestAt) {
		t.Fatal("request at time was used instead of the confirmed claim time")
	}

	var raw map[string]any
	if err := json.Unmarshal(issued.Doc, &raw); err != nil {
		t.Fatal(err)
	}
	if err := credential.Verify(raw, issuer.PublicKeyMultibase()); err != nil {
		t.Fatalf("issued credential signature failed verification: %v", err)
	}
	var doc schema.WorkEventCredential
	if err := json.Unmarshal(issued.Doc, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.ValidFrom.Equal(claimAt) || !doc.CredentialSubject.Confirmation.At.Equal(claimAt) {
		t.Fatalf("signed timestamps did not use claim confirmation: validFrom=%s confirmation=%v", doc.ValidFrom, doc.CredentialSubject.Confirmation.At)
	}
	if got := doc.CredentialSubject.WorkEvent.EvidenceFields; !reflect.DeepEqual(got, fields) {
		t.Fatalf("signed evidence fields = %#v, want %#v", got, fields)
	}
}

func evidenceClient(base string) *client.Client { return client.New(base) }
