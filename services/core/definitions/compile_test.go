package definitions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

var testTime = time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

// fullDraft is a draft with every section answered — the wizard walked to the
// end. Tests knock pieces out of it and assert the named refusal.
func fullDraft() DraftDoc {
	delay := 3
	return DraftDoc{
		Scope: &ScopeSection{Sector: "community-health", Category: "household-visit"},
		Activity: &ActivitySection{
			Code:        "chw.household.visit",
			Label:       "Household visit",
			OutcomeUnit: "visits",
			Counting: &schema.DefinitionCounting{
				Basis:            schema.DefinitionCountingBasisEvent,
				Frequency:        strPtr("monthly"),
				AggregationLevel: strPtr("per-worker"),
			},
		},
		Parties: &PartiesSection{
			PerformerRole:     "community-health-worker",
			PartyType:         "person",
			AttesterFunctions: []string{"submit-work-evidence"},
		},
		Evidence: &EvidenceSection{
			Summary:                 "Visit a household, record it the same day",
			EvidenceInPlainLanguage: []string{"the visit appears in the programme system"},
			TierCeiling:             2,
			CheckIntensity:          "sampled",
			TierMap: []schema.DefinitionTierMapItem{
				{
					Tier:            2,
					SourceClassIn:   []schema.SourceClass{schema.SourceClassProgrammeSystem},
					CaptureMethodIn: []schema.CaptureMethod{schema.CaptureMethodDigitalCapture},
					RequiresFields:  []string{"household_id"},
				},
				{
					Tier:            3,
					SourceClassIn:   []schema.SourceClass{schema.SourceClassSupervisedCapture},
					CaptureMethodIn: []schema.CaptureMethod{schema.CaptureMethodSupervisedManual},
				},
			},
		},
		Validation: &ValidationSection{
			AuthorisedIssuers: []string{"did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAD"},
			SpecifierPartyID:  "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAC",
			Posture:           "post-hoc-sampled",
			DelayDays:         &delay,
		},
		Sources: &SourcesSection{
			SourceSystems:  []string{"riverside-dhis2"},
			RequiredFields: []string{"household_id"},
			Connections: []SourceConnection{{
				SystemRef:     "riverside-dhis2",
				AdapterRef:    "csv-batch@1",
				Endpoint:      "https://dhis2.example.org/api",
				CredentialRef: "vault:sources/riverside-dhis2",
				Settings:      map[string]string{"orgUnit": "riverside"},
			}},
		},
		Cascade: &CascadeSection{
			RoleLevel:             "frontline",
			TrainedByDefinitionID: "crest:definition:01BX5ZZKBKACTAV9WEVGEMMVRZ",
			TrainedByVersion:      1,
		},
		Extensions: map[string]ExtensionField{
			"supervision_radius_km": {Label: "Supervision radius (km)", ValueType: "number", Value: "12"},
		},
		Payment: &PaymentSection{
			Roles:         map[string]string{"author": "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", "rate-owner": "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAE"},
			Tranches:      []Tranche{{Label: "on confirmation", Share: "60%"}, {Label: "on validation", Share: "40%"}},
			Preconditions: []string{"training complete"},
			Deductions:    []Deduction{{Label: "late reporting", Rule: "reported more than 30 days after the visit"}},
		},
	}
}

func strPtr(s string) *string { return &s }

// A complete draft compiles with no problems, and the result satisfies the
// definition schema — the same check submit runs, so this is the guarantee
// that the wizard can actually finish.
func TestCompileCompleteDraftValidates(t *testing.T) {
	d, problems := compile(fullDraft(), "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", testTime)
	if len(problems) != 0 {
		t.Fatalf("a complete draft has problems: %+v", problems)
	}
	if err := schema.Validate(schema.IDDefinition, d); err != nil {
		t.Fatalf("compiled definition fails its own schema: %v", err)
	}
	if d.State != schema.DefinitionStateDRAFT {
		t.Fatalf("compiled into %s, not DRAFT: a submit must never mint anything past DRAFT", d.State)
	}
	if d.Classification["sector"] != "community-health" {
		t.Errorf("sector did not reach classification: %v", d.Classification)
	}
	if d.Counting == nil || d.Counting.Basis != schema.DefinitionCountingBasisEvent {
		t.Errorf("counting basis lost in compilation")
	}
}

func TestCompileRefusesUnregisteredEvidenceSchema(t *testing.T) {
	doc := fullDraft()
	doc.Sources.SchemaRef = "csv-batch@1"
	_, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	for _, p := range problems {
		if p.Section == "sources" && p.Field == "schemaRef" && strings.Contains(p.Reason, "not registered") {
			return
		}
	}
	t.Fatalf("unregistered evidence schema compiled without a named problem: %+v", problems)
}

// Every missing section is a named open question, not a refusal to answer —
// p3_18 renders this list.
func TestCompileNamesEveryOpenSection(t *testing.T) {
	_, problems := compile(DraftDoc{}, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	sections := map[string]bool{}
	for _, p := range problems {
		sections[p.Section] = true
	}
	for _, want := range []string{"activity", "scope", "parties", "evidence", "sources", "validation"} {
		if !sections[want] {
			t.Errorf("empty draft has no open question for section %q; got %+v", want, problems)
		}
	}
}

// A tier rule above the worker face's ceiling is the two faces of one record
// disagreeing, and it is refused by name.
func TestCompileRefusesTierAboveCeiling(t *testing.T) {
	doc := fullDraft()
	doc.Evidence.TierMap = append(doc.Evidence.TierMap, schema.DefinitionTierMapItem{
		Tier:            1,
		SourceClassIn:   []schema.SourceClass{schema.SourceClassNationalSystem},
		CaptureMethodIn: []schema.CaptureMethod{schema.CaptureMethodSystemOfRecord},
	})
	_, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	if len(problems) == 0 {
		t.Fatal("a tier rule above the ceiling compiled without complaint")
	}
	if !strings.Contains(problems[0].Reason, "ceiling") {
		t.Errorf("refusal does not name the ceiling: %+v", problems[0])
	}
}

// Connection settings that look like secrets are refused: CREST stores a
// credentialRef naming where the secret lives, never the secret (p3_26).
func TestCompileRefusesSecretsInConnections(t *testing.T) {
	for _, key := range []string{"password", "api_key", "apiKey", "client_secret", "Authorization", "bearerToken"} {
		doc := fullDraft()
		doc.Sources.Connections[0].Settings = map[string]string{key: "hunter2"}
		_, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
		found := false
		for _, p := range problems {
			if strings.Contains(p.Field, key) && strings.Contains(p.Reason, "secret") {
				found = true
			}
		}
		if !found {
			t.Errorf("setting key %q was not refused as a secret: %+v", key, problems)
		}
	}
}

// An extension value must read as its declared type; a typed escape hatch
// that does not check is an untyped one.
func TestCompileChecksExtensionTypes(t *testing.T) {
	cases := map[string]ExtensionField{
		"bad_number":  {Label: "n", ValueType: "number", Value: "twelve"},
		"bad_boolean": {Label: "b", ValueType: "boolean", Value: "yep"},
		"bad_date":    {Label: "d", ValueType: "date", Value: "next tuesday"},
		"bad_type":    {Label: "t", ValueType: "money", Value: "10"},
	}
	for key, field := range cases {
		doc := fullDraft()
		doc.Extensions = map[string]ExtensionField{key: field}
		_, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
		found := false
		for _, p := range problems {
			if p.Section == "extensions" && p.Field == key {
				found = true
			}
		}
		if !found {
			t.Errorf("extension %q (%+v) was not refused", key, field)
		}
	}

	// And the well-typed ones pass.
	doc := fullDraft()
	doc.Extensions = map[string]ExtensionField{
		"ok_string":  {Label: "s", ValueType: "string", Value: "anything"},
		"ok_number":  {Label: "n", ValueType: "number", Value: "12.5"},
		"ok_boolean": {Label: "b", ValueType: "boolean", Value: "true"},
		"ok_date":    {Label: "d", ValueType: "date", Value: "2026-09-03"},
	}
	if _, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime); len(problems) != 0 {
		t.Errorf("well-typed extensions refused: %+v", problems)
	}
}

// A definition cannot be its own training prerequisite.
func TestCompileRefusesSelfCascade(t *testing.T) {
	doc := fullDraft()
	doc.Cascade.TrainedByDefinitionID = "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	if len(problems) == 0 {
		t.Fatal("a self-referential cascade compiled without complaint")
	}
}

// Ratification: separation of duties holds, and pending fields are recorded
// deduplicated, under the ratifier's hand only.
func TestApplyRatification(t *testing.T) {
	d := schema.Definition{AuthoredByPartyID: "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA"}
	if err := applyRatification(&d, "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", nil); !errors.Is(err, ErrSelfRatified) {
		t.Fatalf("self-ratification returned %v, not ErrSelfRatified", err)
	}

	d = schema.Definition{AuthoredByPartyID: "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA"}
	err := applyRatification(&d, "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAB",
		[]string{"rate", " rate ", "", "mechanism owner"})
	if err != nil {
		t.Fatal(err)
	}
	if *d.RatifiedByPartyID != "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAB" {
		t.Errorf("ratifier not recorded")
	}
	if len(d.PendingFields) != 2 || d.PendingFields[0] != "rate" || d.PendingFields[1] != "mechanism owner" {
		t.Errorf("pending fields not deduplicated and trimmed: %v", d.PendingFields)
	}
}

// Ratifying with no pending fields leaves the field absent — a version with
// nothing pending must not carry an empty list that reads as a declaration.
func TestRatificationWithoutPendingLeavesFieldAbsent(t *testing.T) {
	d := schema.Definition{AuthoredByPartyID: "a"}
	if err := applyRatification(&d, "b", nil); err != nil {
		t.Fatal(err)
	}
	if d.PendingFields != nil {
		t.Errorf("pendingFields is %v, want absent", d.PendingFields)
	}
}

// Cloning: a wizard document rebuilt from a compiled definition compiles back
// to the same definition. This is what makes "Clone a version" (p3_1) safe —
// the copy carries everything, and the original is never touched.
func TestCloneRoundTrip(t *testing.T) {
	original, problems := compile(fullDraft(), "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", testTime)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	doc := docFromDefinition(original)
	clone, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 2, "did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", testTime)
	if len(problems) != 0 {
		t.Fatalf("a cloned draft has problems: %+v", problems)
	}
	clone.Version = original.Version // the one intended difference
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(clone)
	if string(a) != string(b) {
		t.Errorf("clone round trip diverged:\noriginal: %s\nclone:    %s", a, b)
	}
}

// setSection refuses a section the wizard does not have, and routes the ones
// it does.
func TestSetSection(t *testing.T) {
	var doc DraftDoc
	if err := setSection(&doc, "scope", json.RawMessage(`{"sector":"health"}`)); err != nil {
		t.Fatal(err)
	}
	if doc.Scope == nil || doc.Scope.Sector != "health" {
		t.Errorf("scope write did not land: %+v", doc.Scope)
	}
	if err := setSection(&doc, "payments", json.RawMessage(`{}`)); err == nil {
		t.Fatal("an unknown section name was accepted")
	}
	if err := setSection(&doc, "scope", json.RawMessage(`{"sector":`)); err == nil {
		t.Fatal("an unparseable section body was accepted")
	}
}

// A draft that has never been submitted has no definition id, and the two
// endpoints that compile without writing — validate and dry-run — stand a
// placeholder in for one. That placeholder is schema-checked along with the
// rest of the preview, so if it does not satisfy the id pattern, `validate`
// reports a schema problem on EVERY new draft: an open question naming no
// section a person could go back to, because there is no field for it.
//
// The consequence is worse than a spurious row. `submit` mints a real id and
// so never sees the violation, meaning validate refuses drafts submit would
// accept — and the property this whole design rests on is that the two run the
// same compile and cannot disagree. That is what this test defends, and the
// original placeholder ("crest:definition:PREVIEW") failed it, invisibly,
// because every unit test called compile with a real id.
func TestPreviewIDLetsACompleteDraftReadAsReady(t *testing.T) {
	d, problems := compile(fullDraft(), previewDefinitionID, 1,
		"did:crest:party:01ARZ3NDEKTSV4RRFFQ69G5FAA", testTime)
	if len(problems) != 0 {
		t.Fatalf("a complete draft has problems under the preview id: %+v", problems)
	}
	if err := schema.Validate(schema.IDDefinition, d); err != nil {
		t.Fatalf("the preview id is not schema-valid, so validate would report a problem "+
			"no author can fix and submit would never see: %v", err)
	}
}
