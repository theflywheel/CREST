package csv_test

import (
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	adaptercsv "github.com/theflywheel/crest/adapters/csv"
)

// receivedAt is passed in rather than read from the clock (adapters.Adapter),
// so every test uses the same instant and a timestamp difference means the
// adapter invented one.
var receivedAt = time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)

// A signed batch from a programme system: what a deployment configures once,
// when the source is onboarded and assessed. Never read out of the payload.
var source = adapters.Source{
	Class:         "programme-system",
	CaptureMethod: "digital-capture",
	Exposure:      "signed-batch",
	SystemRef:     "riverside-dhis2",
}

const header = "activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,period_end\n"

func parse(t *testing.T, body string) ([]adapters.Row, []adapters.Rejection) {
	t.Helper()
	rows, rejected, err := adaptercsv.Adapter{}.Parse(strings.NewReader(body), source, receivedAt)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return rows, rejected
}

// A row is either a record or a rejection; the adapter has no third outcome.
// If this drifts, a batch can lose rows silently — three people paid nothing,
// and nobody told which three (adapters.Rejection).
func TestEveryDataRowIsEitherARecordOrARejection(t *testing.T) {
	body := header +
		"bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n" +
		"bednet-distribution,oops,bednets-distributed,phone,+15550100012,2026-03-02,\n" +
		"bednet-distribution,3,bednets-distributed,roster-id,riverside-roster-0003,2026-03-03,\n"

	rows, rejected := parse(t, body)
	if got := len(rows) + len(rejected); got != 3 {
		t.Fatalf("3 data rows in, %d accounted for (%d records, %d rejections)",
			got, len(rows), len(rejected))
	}
}

// A rejection a person cannot find in the file they sent is a rejection they
// cannot act on. The ref is the only thing connecting the two.
func TestARejectionNamesTheRowInTheFile(t *testing.T) {
	body := header +
		"bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n" +
		"bednet-distribution,-4,bednets-distributed,phone,+15550100012,2026-03-02,\n"

	_, rejected := parse(t, body)
	if len(rejected) != 1 {
		t.Fatalf("want 1 rejection, got %d", len(rejected))
	}
	// Header is line 1, the good row line 2, the bad row line 3 — counted the
	// way the person looking at the file in a spreadsheet counts.
	if rejected[0].Ref != "row 3" {
		t.Errorf("rejection ref = %q, want %q", rejected[0].Ref, "row 3")
	}
	if rejected[0].Reason == "" {
		t.Error("rejection has no reason; a row refused without a reason cannot be corrected")
	}
}

// Records carry their own ref for the same reason rejections do: a record that
// is later disputed has to be traceable back to the line it came from.
func TestAnAcceptedRowAlsoCarriesItsRef(t *testing.T) {
	rows, _ := parse(t, header+"bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n")
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Ref != "row 2" {
		t.Errorf("row ref = %q, want %q", rows[0].Ref, "row 2")
	}
}

// One bad value must cost one row, never the batch. Every case here describes
// work that did not happen as stated; the work on the other lines still did,
// and refusing all of it is a person unpaid for someone else's typo.
func TestABadValueRejectsOnlyItsOwnRow(t *testing.T) {
	good := "bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n"

	cases := []struct {
		name          string
		row           string
		reasonMention string
	}{
		{
			name:          "a non-numeric outcome cannot become a quantity",
			row:           "bednet-distribution,twelve,bednets-distributed,phone,+15550100012,2026-03-02,\n",
			reasonMention: "outcome_value",
		},
		{
			name:          "a negative outcome is work undone, which the schema has no meaning for",
			row:           "bednet-distribution,-4,bednets-distributed,phone,+15550100012,2026-03-02,\n",
			reasonMention: "negative",
		},
		{
			name:          "an unparseable start date leaves the record with no period",
			row:           "bednet-distribution,4,bednets-distributed,phone,+15550100012,the second of March,\n",
			reasonMention: "period_start",
		},
		{
			name:          "an unparseable end date is not silently dropped to leave an open period",
			row:           "bednet-distribution,4,bednets-distributed,phone,+15550100012,2026-03-02,soon\n",
			reasonMention: "period_end",
		},
		{
			name:          "a period that ends before it starts describes no interval at all",
			row:           "bednet-distribution,4,bednets-distributed,phone,+15550100012,2026-03-05,2026-03-04\n",
			reasonMention: "before it starts",
		},
		{
			name:          "with no worker identifier there is nobody to pay",
			row:           "bednet-distribution,4,bednets-distributed,phone,,2026-03-02,\n",
			reasonMention: "worker identifier",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, rejected := parse(t, header+good+c.row)
			if len(rows) != 1 {
				t.Fatalf("the good row was not kept: %d records", len(rows))
			}
			if len(rejected) != 1 {
				t.Fatalf("want 1 rejection, got %d", len(rejected))
			}
			if !strings.Contains(rejected[0].Reason, c.reasonMention) {
				t.Errorf("reason %q does not mention %q — a reason that does not name the "+
					"problem is a reason nobody can fix", rejected[0].Reason, c.reasonMention)
			}
		})
	}
}

// A missing mandatory column is not a per-row problem: every row in the file
// lacks it, so the file cannot be turned into evidence at all. Failing whole
// and naming the column is the only outcome a sender can act on.
func TestAMissingMandatoryColumnFailsTheWholeFileAndNamesTheColumn(t *testing.T) {
	cases := map[string]string{
		"activity":      "outcome_value,outcome_unit,worker_id,period_start\n12,bednets-distributed,+15550100011,2026-03-02\n",
		"outcome_value": "activity,outcome_unit,worker_id,period_start\nbednet-distribution,bednets-distributed,+15550100011,2026-03-02\n",
		"outcome_unit":  "activity,outcome_value,worker_id,period_start\nbednet-distribution,12,+15550100011,2026-03-02\n",
		"worker_id":     "activity,outcome_value,outcome_unit,period_start\nbednet-distribution,12,bednets-distributed,2026-03-02\n",
		"period_start":  "activity,outcome_value,outcome_unit,worker_id\nbednet-distribution,12,bednets-distributed,+15550100011\n",
	}

	for column, body := range cases {
		t.Run("no "+column+" column", func(t *testing.T) {
			rows, rejected, err := adaptercsv.Adapter{}.Parse(strings.NewReader(body), source, receivedAt)
			if err == nil {
				t.Fatalf("the file was accepted with no %q column: %d records, %d rejections",
					column, len(rows), len(rejected))
			}
			if !strings.Contains(err.Error(), column) {
				t.Errorf("error %q does not name the missing column %q", err, column)
			}
			if rows != nil {
				t.Error("records were returned alongside a whole-file failure; a partial batch " +
					"from an unusable file is worse than none")
			}
		})
	}
}

// Source systems export dates and timestamps both, and a programme with only a
// date-granular export is exactly the programme this adapter exists for.
func TestBothADateAndAnRFC3339TimestampParse(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Time
	}{
		{"a bare date starts at midnight UTC", "2026-03-02", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)},
		{"an RFC3339 timestamp keeps its time of day", "2026-03-02T14:30:00Z", time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC)},
		{"an offset timestamp is normalised to UTC", "2026-03-02T14:30:00+05:30", time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, rejected := parse(t,
				header+"bednet-distribution,12,bednets-distributed,phone,+15550100011,"+c.value+",\n")
			if len(rejected) != 0 {
				t.Fatalf("rejected %q: %s", c.value, rejected[0].Reason)
			}
			if got := rows[0].Record.Period.Start; !got.Equal(c.want) {
				t.Errorf("period.start = %s, want %s", got, c.want)
			}
		})
	}
}

// An unrecognised column is information CREST does not happen to model yet —
// and a definition's tier map may require exactly that field to award anything
// above the floor. Discarding it silently caps evidence for a reason nobody
// can see.
func TestUnrecognisedColumnsAreKeptVerbatimInEnrichment(t *testing.T) {
	rows, rejected := parse(t,
		"activity,outcome_value,outcome_unit,worker_id,period_start,beneficiary_count,Supervisor Present\n"+
			"bednet-distribution,12,bednets-distributed,+15550100011,2026-03-02,4,yes\n")
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejection: %s", rejected[0].Reason)
	}

	enrichment := rows[0].Record.Enrichment
	if got := enrichment["beneficiary_count"]; got != "4" {
		t.Errorf("enrichment[beneficiary_count] = %v, want %q — kept verbatim means the string "+
			"the source sent, not a value CREST has decided how to interpret", got, "4")
	}
	// Header keys are lowercased and trimmed so a tier map can name a field
	// without depending on how the exporter capitalised it.
	if got := enrichment["supervisor present"]; got != "yes" {
		t.Errorf("enrichment[supervisor present] = %v, want %q", got, "yes")
	}
}

// The optional columns are optional: a record with only the mandatory core is
// valid Tier-1-capable evidence (§8), so their absence must not become an
// empty string masquerading as a value.
func TestOmittedOptionalFieldsAreAbsentRatherThanEmpty(t *testing.T) {
	rows, rejected := parse(t,
		"activity,outcome_value,outcome_unit,worker_id,period_start\n"+
			"bednet-distribution,12,bednets-distributed,+15550100011,2026-03-02\n")
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejection: %s", rejected[0].Reason)
	}

	rec := rows[0].Record
	if rec.Period.End != nil {
		t.Errorf("period.end = %v; a file that did not say when the work ended must not have one invented", rec.Period.End)
	}
	if rec.Geography != nil {
		t.Errorf("geography = %v, want none", *rec.Geography)
	}
	if rec.Provenance.SourceRecordRef != nil {
		t.Errorf("sourceRecordRef = %v, want none", *rec.Provenance.SourceRecordRef)
	}
	if rec.Enrichment != nil {
		t.Errorf("enrichment = %v, want none", rec.Enrichment)
	}
	// Absent worker_id_kind falls back to phone, which is what a batch file
	// with a bare identifier column almost always carries.
	if rec.WorkerJoiningIdentifier.Kind != "phone" {
		t.Errorf("worker identifier kind = %q, want phone", rec.WorkerJoiningIdentifier.Kind)
	}
}

// receivedAt is an argument so a harness running a seven-day window in
// milliseconds produces records whose timestamps agree with the rest of the
// run. An adapter that reached for time.Now() would make that run incoherent.
func TestReceivedAtComesFromTheCallerNotTheClock(t *testing.T) {
	rows, _ := parse(t, header+"bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n")
	if got := rows[0].Record.Provenance.ReceivedAt; !got.Equal(receivedAt) {
		t.Errorf("receivedAt = %s, want the caller's %s", got, receivedAt)
	}
}

// The ref ends up in every record's provenance, and the adapter registry
// records which versions are recognised. A ref that drifts from the constant
// is a verifier resolving evidence to a translator that does not exist.
func TestRefIsTheVersionedAdapterIdentity(t *testing.T) {
	if got := (adaptercsv.Adapter{}).Ref(); got != adaptercsv.Version {
		t.Errorf("Ref() = %q, want %q", got, adaptercsv.Version)
	}
	if !strings.Contains(adaptercsv.Version, "@") {
		t.Errorf("adapter ref %q carries no version; %q is not resolvable to a translator",
			adaptercsv.Version, adaptercsv.Version)
	}
}

// An empty file is not a batch of zero rows to be shrugged at — there is no
// header, so nothing about the file's shape is known.
func TestAFileWithNoHeaderFailsRatherThanReturningNothing(t *testing.T) {
	_, _, err := adaptercsv.Adapter{}.Parse(strings.NewReader(""), source, receivedAt)
	if err == nil {
		t.Fatal("an empty file was accepted; a batch that parsed to silence is indistinguishable " +
			"from a batch that was never sent")
	}
}

// A header with no data rows is a real thing a source system sends, and it is
// not an error — it is a batch reporting that nothing happened.
func TestAHeaderWithNoRowsIsAnEmptyBatchNotAFailure(t *testing.T) {
	rows, rejected, err := adaptercsv.Adapter{}.Parse(strings.NewReader(header), source, receivedAt)
	if err != nil {
		t.Fatalf("a well-formed empty batch failed: %v", err)
	}
	if len(rows) != 0 || len(rejected) != 0 {
		t.Errorf("got %d records and %d rejections from a file with no data rows", len(rows), len(rejected))
	}
}

// A short row means a column was dropped somewhere upstream, and guessing
// which one is how a worker's outcome becomes someone else's. It is refused —
// but only it, because the other lines describe work that happened.
func TestAStructurallyBrokenRowIsRejectedWithoutStoppingTheBatch(t *testing.T) {
	body := header +
		"bednet-distribution,12,bednets-distributed,phone,+15550100011,2026-03-02,\n" +
		"bednet-distribution,9,bednets-distributed,phone\n" +
		"bednet-distribution,3,bednets-distributed,phone,+15550100012,2026-03-03,\n"

	rows, rejected := parse(t, body)
	if len(rows) != 2 {
		t.Errorf("want the 2 well-formed rows kept, got %d", len(rows))
	}
	if len(rejected) != 1 {
		t.Fatalf("want 1 rejection, got %d", len(rejected))
	}
	if !strings.Contains(rejected[0].Ref, "row") {
		t.Errorf("rejection ref = %q, which does not locate the row in the file", rejected[0].Ref)
	}
}

// A quoted field containing a newline makes one record span two physical
// lines. A counter drifts from that point on, and every later rejection then
// points at the wrong row — which breaks the one property the reference exists
// for: that a person can find it in the file they sent.
func TestARowReferencePointsAtTheRealLineAfterAnEmbeddedNewline(t *testing.T) {
	file := "activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,note\n" +
		"bednet-distribution,4,bednets-distributed,phone,+15550100011,2026-03-02,\"first line\nsecond line\"\n" +
		"bednet-distribution,notanumber,bednets-distributed,phone,+15550100012,2026-03-02,fine\n"

	_, rejected, err := adaptercsv.Adapter{}.Parse(strings.NewReader(file), source, receivedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 {
		t.Fatalf("expected one rejection, got %d: %+v", len(rejected), rejected)
	}
	// The bad record starts on physical line 4, because the record above it
	// occupies lines 2 and 3.
	if rejected[0].Ref != "row 4" {
		t.Errorf("rejection points at %q; the bad record starts on line 4 of the file",
			rejected[0].Ref)
	}
}
