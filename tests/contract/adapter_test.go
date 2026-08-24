package contract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	adaptercsv "github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/schema"
)

// The instant the batch was received, passed in rather than read, so a run
// that drives the clock produces records agreeing with the rest of the run.
var batchReceivedAt = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

// What the deployment knows about the source: established when the source was
// onboarded and assessed, held in configuration. Nothing in any file below can
// change any of it, and that is the point of every test in this file.
var configuredSource = adapters.Source{
	Class:         schema.SourceClassProgrammeSystem,
	CaptureMethod: schema.CaptureMethodDigitalCapture,
	Exposure:      schema.SourceExposureSignedBatch,
	SystemRef:     "riverside-dhis2",
}

func fixtureReader(t *testing.T, name string) *bytes.Reader {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "fixtures", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return bytes.NewReader(raw)
}

func parseFixture(t *testing.T, name string) ([]adapters.Row, []adapters.Rejection) {
	t.Helper()
	rows, rejected, err := adaptercsv.Adapter{}.Parse(fixtureReader(t, name), configuredSource, batchReceivedAt)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return rows, rejected
}

// The single most important test here. §8: provenance is adapter-attached and
// pipeline-validated; whatever the source asserts about its own
// trustworthiness is ignored. A file claiming national-system /
// system-of-record would otherwise buy itself a tier it has no basis for, and
// every trust judgement downstream would be built on a column anyone can type.
func TestASourceCannotAssertItsOwnProvenance(t *testing.T) {
	rows, rejected := parseFixture(t, "csv-asserting-its-own-provenance.csv")
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejection: %s", rejected[0].Reason)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 record, got %d", len(rows))
	}
	rec := rows[0].Record

	// The file says national-system / system-of-record / trust-me@9 / push-api.
	// Every one of those must have been ignored in favour of configuration.
	if rec.Provenance.SourceClass != configuredSource.Class {
		t.Errorf("sourceClass = %q, want the configured %q — the file's claim won (§8)",
			rec.Provenance.SourceClass, configuredSource.Class)
	}
	if rec.Provenance.CaptureMethod != configuredSource.CaptureMethod {
		t.Errorf("captureMethod = %q, want the configured %q", rec.Provenance.CaptureMethod, configuredSource.CaptureMethod)
	}
	if rec.Provenance.SourceExposure != configuredSource.Exposure {
		t.Errorf("sourceExposure = %q, want the configured %q", rec.Provenance.SourceExposure, configuredSource.Exposure)
	}
	if rec.Provenance.AdapterRef != adaptercsv.Version {
		t.Errorf("adapterRef = %q, want the adapter's own %q — a record naming a translator "+
			"the file chose is a record no verifier can resolve", rec.Provenance.AdapterRef, adaptercsv.Version)
	}

	// The claims are not deleted either: they are unrecognised columns like any
	// other, kept verbatim where they are visibly *data*, not provenance. That a
	// source tried to assert its own trust level is itself worth knowing.
	for column, claimed := range map[string]string{
		"source_class":    "national-system",
		"capture_method":  "system-of-record",
		"adapter_ref":     "trust-me@9",
		"source_exposure": "push-api",
	} {
		if got := rec.Enrichment[column]; got != claimed {
			t.Errorf("enrichment[%s] = %v, want %q kept verbatim", column, got, claimed)
		}
	}
}

// §8 says a record providing only the mandatory core is valid Tier-1-capable
// evidence. If this record fails validation that sentence is false, and the
// programme with no API — the one this adapter exists for — has no way in.
func TestARecordWithOnlyTheMandatoryCoreIsValid(t *testing.T) {
	rec := schema.CanonicalWorkEvidenceRecord{
		Activity: "bednet-distribution",
		Outcome:  schema.Outcome{Value: 6, Unit: "bednets-distributed"},
		WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
			Kind:  schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone,
			Value: "+15550100011",
		},
		Period: schema.Period{Start: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)},
		Provenance: schema.Provenance{
			SourceClass:    schema.SourceClassSelfReported,
			CaptureMethod:  schema.CaptureMethodUnsupervisedManual,
			SourceExposure: schema.SourceExposureSignedBatch,
			AdapterRef:     adaptercsv.Version,
			ReceivedAt:     batchReceivedAt,
		},
	}
	if err := schema.Validate(schema.IDEvidenceRecord, rec); err != nil {
		t.Fatalf("the mandatory core alone did not validate: %v", err)
	}
}

// And the adapter must actually be able to produce that record from the
// minimum file: the guarantee is worthless if it holds only for a struct built
// by hand in a test.
func TestTheAdapterCanProduceTheMandatoryCoreFromTheMinimumFile(t *testing.T) {
	rows, rejected := parseFixture(t, "csv-with-only-the-mandatory-core.csv")
	if len(rejected) != 0 {
		t.Fatalf("the minimum viable file was rejected: %s", rejected[0].Reason)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 record, got %d", len(rows))
	}
	if err := schema.Validate(schema.IDEvidenceRecord, rows[0].Record); err != nil {
		t.Fatalf("%s: %v", rows[0].Ref, err)
	}
}

// An adapter that emits a record the pipeline will later reject is a bug in
// the adapter, and one that surfaces as a worker's evidence disappearing
// somewhere between ingestion and payment.
func TestEveryEmittedRecordValidatesAgainstTheCanonicalSchema(t *testing.T) {
	for _, fixture := range []string{
		"bednet-batch-clean.csv",
		"csv-with-unmatched-rows.csv",
		"csv-asserting-its-own-provenance.csv",
		"csv-with-only-the-mandatory-core.csv",
	} {
		t.Run(fixture, func(t *testing.T) {
			rows, _ := parseFixture(t, fixture)
			if len(rows) == 0 {
				t.Fatal("no records emitted; a fixture proving nothing is a fixture that will rot")
			}
			for _, row := range rows {
				if err := schema.Validate(schema.IDEvidenceRecord, row.Record); err != nil {
					t.Errorf("%s: %v", row.Ref, err)
				}
			}
		})
	}
}

// Nothing is silently dropped: every data row leaves as a record or as a named
// rejection. A batch that quietly loses three rows pays three people nothing
// and tells no one.
func TestNoRowIsSilentlyDropped(t *testing.T) {
	cases := map[string]struct{ dataRows, wantRecords int }{
		// Three clean rows, three records.
		"bednet-batch-clean.csv": {dataRows: 3, wantRecords: 3},
		// Two good rows around six unusable ones, each unusable for a different
		// reason: a non-numeric outcome, a negative outcome, an unparseable
		// date, a period that ends before it starts, a missing worker.
		"csv-with-unmatched-rows.csv": {dataRows: 7, wantRecords: 2},
	}

	for fixture, want := range cases {
		t.Run(fixture, func(t *testing.T) {
			rows, rejected := parseFixture(t, fixture)
			if got := len(rows) + len(rejected); got != want.dataRows {
				t.Fatalf("%d data rows in, %d accounted for (%d records, %d rejections)",
					want.dataRows, got, len(rows), len(rejected))
			}
			if len(rows) != want.wantRecords {
				t.Errorf("want %d records, got %d", want.wantRecords, len(rows))
			}
			for _, r := range rejected {
				if !strings.HasPrefix(r.Ref, "row ") {
					t.Errorf("rejection ref %q does not locate the row in the file the sender has", r.Ref)
				}
				if r.Reason == "" {
					t.Errorf("%s was refused with no reason", r.Ref)
				}
			}
		})
	}
}

// The refs must be the actual line numbers, in order, or "row 4" sends someone
// to the wrong line of their own file — which is worse than no ref at all.
func TestRejectionRefsPointAtTheRightLines(t *testing.T) {
	_, rejected := parseFixture(t, "csv-with-unmatched-rows.csv")
	want := []string{"row 3", "row 4", "row 5", "row 6", "row 7"}
	if len(rejected) != len(want) {
		t.Fatalf("want %d rejections, got %d", len(want), len(rejected))
	}
	for i, ref := range want {
		if rejected[i].Ref != ref {
			t.Errorf("rejection %d ref = %q, want %q (reason: %s)", i, rejected[i].Ref, ref, rejected[i].Reason)
		}
	}
}

// A source system's extra columns are information, and a definition's tier map
// may require one of them to award anything above the floor. Dropping them
// caps a worker's evidence for a reason nobody can see.
func TestUnrecognisedColumnsSurviveIntoEnrichment(t *testing.T) {
	rows, _ := parseFixture(t, "bednet-batch-clean.csv")
	if len(rows) < 1 {
		t.Fatal("no records")
	}
	first := rows[0].Record
	if got := first.Enrichment["beneficiary_count"]; got != "4" {
		t.Errorf("enrichment[beneficiary_count] = %v, want %q", got, "4")
	}
	if got := first.Enrichment["household_id"]; got != "HH-0007" {
		t.Errorf("enrichment[household_id] = %v, want %q", got, "HH-0007")
	}
	// The columns the adapter does understand belong in their canonical fields,
	// not duplicated into enrichment where a tier map could read a second copy.
	for _, known := range []string{"activity", "outcome_value", "worker_id", "period_start", "geography"} {
		if _, ok := first.Enrichment[known]; ok {
			t.Errorf("enrichment carries %q, which is a canonical field", known)
		}
	}
}

// A missing mandatory column is a property of the file, not of a row: it fails
// whole, and the error names the column so the sender can fix the export
// rather than guess at it.
func TestAMissingMandatoryColumnFailsTheFileByName(t *testing.T) {
	rows, rejected, err := adaptercsv.Adapter{}.Parse(
		fixtureReader(t, "csv-missing-a-mandatory-column.csv"), configuredSource, batchReceivedAt)
	if err == nil {
		t.Fatalf("a file with no outcome_unit column was accepted: %d records, %d rejections",
			len(rows), len(rejected))
	}
	if !strings.Contains(err.Error(), "outcome_unit") {
		t.Errorf("error %q does not name the missing column", err)
	}
}

// The transports are recorded and deliberately not an input to strength (§8),
// but they are recorded — which means the same file arriving by a different
// transport must produce records differing in exactly that one field.
func TestTheSameFileByADifferentTransportDiffersOnlyInExposure(t *testing.T) {
	byBatch, _ := parseFixture(t, "bednet-batch-clean.csv")

	uploaded := configuredSource
	uploaded.Exposure = schema.SourceExposureSupervisedUpload
	byUpload, _, err := adaptercsv.Adapter{}.Parse(
		fixtureReader(t, "bednet-batch-clean.csv"), uploaded, batchReceivedAt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(byBatch) != len(byUpload) {
		t.Fatalf("transport changed the record count: %d vs %d", len(byBatch), len(byUpload))
	}
	for i := range byBatch {
		a, b := byBatch[i].Record, byUpload[i].Record
		if b.Provenance.SourceExposure != schema.SourceExposureSupervisedUpload {
			t.Errorf("%s: exposure not recorded as sent", byUpload[i].Ref)
		}
		b.Provenance.SourceExposure = a.Provenance.SourceExposure
		// sourceRecordRef is a pointer into each parse; compare what it says.
		if deref(a.Provenance.SourceRecordRef) != deref(b.Provenance.SourceRecordRef) {
			t.Errorf("%s: transport changed the source record ref", byBatch[i].Ref)
		}
		b.Provenance.SourceRecordRef = a.Provenance.SourceRecordRef
		if a.Provenance != b.Provenance {
			t.Errorf("%s: transport changed provenance beyond the exposure field: %+v vs %+v",
				byBatch[i].Ref, a.Provenance, b.Provenance)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// The duplicate golden file (#41). A batch containing the same work twice is
// not an error the adapter should invent an opinion about: the adapter's job is
// translation, and rejecting the second row here would throw away the only copy
// of a record that might be the legitimate one. Deduplication belongs to
// evidence ingest, where there is a transaction and a stored history to compare
// against — see TestTheSameRecordTwiceCollidesOnTheDedupeKey.
//
// What the adapter owes the deduplicator is determinism: two identical input
// rows must translate to records identical in every field the dedupe key reads,
// and a row that differs in the work performed must not.
func TestDuplicateRowsArePassedThroughIdenticallyRatherThanJudged(t *testing.T) {
	rows, rejected := parseFixture(t, "csv-with-the-same-record-twice.csv")
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejection: %s", rejected[0].Reason)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 records — the adapter must not drop the repeat, got %d", len(rows))
	}

	first, repeat, distinct := rows[0].Record, rows[1].Record, rows[2].Record

	key := func(r schema.CanonicalWorkEvidenceRecord) string {
		ref := ""
		if r.Provenance.SourceRecordRef != nil {
			ref = *r.Provenance.SourceRecordRef
		}
		end := ""
		if r.Period.End != nil {
			end = r.Period.End.UTC().Format(time.RFC3339)
		}
		return strings.Join([]string{
			r.Activity, string(r.WorkerJoiningIdentifier.Kind), r.WorkerJoiningIdentifier.Value,
			r.Period.Start.UTC().Format(time.RFC3339), end,
			fmt.Sprintf("%v %s", r.Outcome.Value, r.Outcome.Unit), ref,
		}, "\x1f")
	}

	if key(first) != key(repeat) {
		t.Errorf("two identical rows translated to distinguishable records:\n %q\n %q\n"+
			"a deduplicator downstream cannot catch what the adapter has already made different",
			key(first), key(repeat))
	}
	if key(first) == key(distinct) {
		t.Errorf("a row recording different work translated to the same key as %q — "+
			"deduplication would erase real work", key(first))
	}
}
