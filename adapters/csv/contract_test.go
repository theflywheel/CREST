package csv_test

import (
	"os"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/adapters/contract"
	"github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/schema"
)

// The CSV adapter is held to the same contract every adapter will be: the
// suite is the proof a new adapter package copies, not something CSV-specific.
func TestConnectorContract(t *testing.T) {
	clean, err := os.ReadFile("../../tests/fixtures/bednet-batch-clean.csv")
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := os.ReadFile("../../tests/fixtures/csv-with-unmatched-rows.csv")
	if err != nil {
		t.Fatal(err)
	}
	contract.Run(t, contract.Suite{
		Adapter: csv.Adapter{},
		Source: adapters.Source{
			AdapterRef: csv.Version,
			Class:      schema.SourceClassProgrammeSystem, CaptureMethod: schema.CaptureMethodDigitalCapture,
			Exposure: schema.SourceExposureSignedBatch, SystemRef: "contract-fixture",
		},
		ReceivedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Cases: []contract.Case{
			{Name: "a clean batch", Payload: clean, WantRows: countRows(clean), WantRejections: 0},
			// Five malformed rows: a non-number, a negative, an unparseable date,
			// an end before its start, and a missing joining id. Each is a
			// rejection that names its row; the two good rows still come through.
			{Name: "malformed rows are rejected one by one", Payload: mixed, WantRows: 2, WantRejections: 5},
		},
	})
}

func countRows(b []byte) int {
	n := 0
	for _, line := range splitLines(string(b)) {
		if line != "" {
			n++
		}
	}
	return n - 1 // the header
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
