// Package contract provides a reusable conformance suite for CREST connectors.
package contract

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/pkg/schema"
)

// Case is one representative source payload and its expected accounting.
type Case struct {
	Name           string
	Payload        []byte
	WantRows       int
	WantRejections int
}

// Suite supplies the source-controlled facts and fixtures used to test an
// adapter. At least one case must emit a canonical record.
type Suite struct {
	Adapter    adapters.Adapter
	Source     adapters.Source
	ReceivedAt time.Time
	Cases      []Case
}

// Run checks the invariants every connector owes the evidence pipeline:
// deterministic translation, complete row accounting, schema-valid output,
// deployment-controlled provenance, and actionable rejection details.
func Run(t *testing.T, suite Suite) {
	t.Helper()
	if suite.Adapter == nil {
		t.Fatal("contract suite has no adapter")
	}
	if suite.Adapter.Ref() == "" {
		t.Fatal("adapter ref is empty")
	}
	if len(suite.Cases) == 0 {
		t.Fatal("contract suite has no cases")
	}
	emitted := 0
	for _, tc := range suite.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			parse := func() ([]adapters.Row, []adapters.Rejection) {
				rows, rejected, err := suite.Adapter.Parse(bytes.NewReader(tc.Payload), suite.Source, suite.ReceivedAt)
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				return rows, rejected
			}
			rows, rejected := parse()
			if len(rows) != tc.WantRows || len(rejected) != tc.WantRejections {
				t.Fatalf("accounting = %d rows, %d rejections; want %d, %d",
					len(rows), len(rejected), tc.WantRows, tc.WantRejections)
			}
			emitted += len(rows)
			for _, row := range rows {
				if row.Ref == "" {
					t.Error("emitted row has no source reference")
				}
				if err := schema.Validate(schema.IDEvidenceRecord, row.Record); err != nil {
					t.Errorf("%s: canonical schema: %v", row.Ref, err)
				}
				p := row.Record.Provenance
				if p.AdapterRef != suite.Adapter.Ref() || p.SourceClass != suite.Source.Class ||
					p.CaptureMethod != suite.Source.CaptureMethod || p.SourceExposure != suite.Source.Exposure ||
					!p.ReceivedAt.Equal(suite.ReceivedAt) {
					t.Errorf("%s: connector did not preserve deployment-controlled provenance: %#v", row.Ref, p)
				}
			}
			for _, rejection := range rejected {
				if rejection.Ref == "" || rejection.Reason == "" {
					t.Errorf("rejection must identify its source row and reason: %#v", rejection)
				}
			}
			againRows, againRejected := parse()
			if !reflect.DeepEqual(rows, againRows) || !reflect.DeepEqual(rejected, againRejected) {
				t.Error("the same payload and source configuration produced different output")
			}
		})
	}
	if emitted == 0 {
		t.Fatal("contract suite proves no successful translation")
	}
}
