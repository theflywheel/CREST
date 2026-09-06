package evidence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/schema"
)

type consentTestClock time.Time

func (c consentTestClock) Now() time.Time { return time.Time(c) }

func TestConsiderRequiresAffirmativeConsent(t *testing.T) {
	for _, want := range []struct {
		name    string
		consent string
		blocked bool
	}{
		{name: "none", consent: "NONE", blocked: true},
		{name: "withdrawn", consent: "WITHDRAWN", blocked: true},
		{name: "granted", consent: "GRANTED", blocked: false},
	} {
		t.Run(want.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/resolve" {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(match{
					PartyID: "did:crest:party:01JCREST00000000000000WRKA",
					Key:     "phone", Confidence: 1, EnrolmentConsent: want.consent,
				})
			}))
			defer server.Close()

			now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
			in := &ingestor{registry: client.New(server.URL), clock: consentTestClock(now)}
			sourceRef := "riverside-dhis2"
			recordRef := "row-1"
			record := schema.CanonicalWorkEvidenceRecord{
				Activity: "bednet-distribution",
				Outcome:  schema.Outcome{Value: 3, Unit: "bednets-distributed"},
				Period:   schema.Period{Start: now},
				Provenance: schema.Provenance{
					AdapterRef: "csv-batch@1", CaptureMethod: schema.CaptureMethodDigitalCapture,
					ReceivedAt: now, SourceClass: schema.SourceClassProgrammeSystem,
					SourceExposure:  schema.SourceExposureSignedBatch,
					SourceRecordRef: &recordRef, SystemRef: &sourceRef,
				},
				WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
					Kind:  schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone,
					Value: "+15550100011",
				},
			}
			def := schema.Definition{
				ID: "crest:definition:01JCREST00000000000000DEFX", Version: 1,
				State:       schema.DefinitionStateACTIVE,
				Activity:    schema.DefinitionActivity{Code: "bednet-distribution", Label: "Bednet distribution"},
				OutcomeUnit: "bednets-distributed",
				Faces: schema.DefinitionFaces{
					Platform: schema.DefinitionFacesPlatform{
						SchemaRef: schema.IDEvidenceRecord, SourceSystems: []string{sourceRef},
						RequiredFields: []string{"activity", "outcome_value", "outcome_unit", "period_start", "worker_id", "worker_id_kind", "source_record_ref"},
					},
					Worker: schema.DefinitionFacesWorker{TierCeiling: 3},
				},
				TierMap: []schema.DefinitionTierMapItem{{
					Tier: 1, SourceClassIn: []schema.SourceClass{schema.SourceClassProgrammeSystem},
					CaptureMethodIn: []schema.CaptureMethod{schema.CaptureMethodDigitalCapture},
				}},
			}

			kind, reason, unit, _, claim := in.consider(t.Context(), adapters.Row{Record: record}, def,
				ingestParams{ContextID: "crest:context:01JCREST00000000000000PROJ", Source: adapters.Source{SystemRef: sourceRef}}, now)
			if want.blocked {
				if kind != unclearWithdrawn || reason == "" {
					t.Fatalf("consent %q was accepted: kind=%q reason=%q", want.consent, kind, reason)
				}
				if unit.ID != "" || claim.ID != "" {
					t.Fatalf("consent %q produced stored work despite refusal: unit=%s claim=%s", want.consent, unit.ID, claim.ID)
				}
				return
			}
			if kind != "" || reason != "" || unit.ID == "" || claim.ID == "" {
				t.Fatalf("granted consent did not produce a claimable unit: kind=%q reason=%q unit=%q claim=%q",
					kind, reason, unit.ID, claim.ID)
			}
		})
	}
}

func TestConsentRefusalDoesNotRetainTheRecord(t *testing.T) {
	rawID := "999900001111"
	record := schema.CanonicalWorkEvidenceRecord{
		WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
			Kind:  schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindNationalID,
			Value: rawID,
		},
	}

	stored, err := unclearRecord(unclearWithdrawn, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("consent refusal retained %d bytes of the evidence record", len(stored))
	}

	stored, err = unclearRecord(unclearUnattributed, record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), rawID) {
		t.Fatalf("unattributed record retained the raw national identifier: %s", stored)
	}
}
