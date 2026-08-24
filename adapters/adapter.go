// Package adapters is the contract every source-system adapter satisfies.
//
// CREST absorbs heterogeneity; source systems change nothing (§8). Adapters are
// written per system *class* — one DHIS2 adapter, one CommCare, one HCM, one
// batch-file — and configured per deployment. A per-deployment adapter is a
// per-deployment bug.
//
// The rule this interface exists to enforce: provenance is attached by the
// adapter and validated by the pipeline; whatever the source asserts about its
// own trustworthiness is ignored. Parse takes the provenance as an argument, so
// there is no path by which a column in a file can set it.
package adapters

import (
	"strings"

	"io"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// Source is what the deployment knows about where a batch came from. It is
// configuration, established when the source is onboarded and assessed — never
// read out of the payload.
type Source struct {
	Class         schema.SourceClass
	CaptureMethod schema.CaptureMethod
	Exposure      schema.SourceExposure
	// SystemRef names the source system instance, so an assessment can be
	// attached to it later and downgrade everything it produced (§6).
	SystemRef string

	// Mapping is how this source's own vocabulary reaches the canonical record.
	//
	// It is configuration and could not be anything else: what a source system
	// calls its columns is the definition of a thing two deployments disagree
	// about while both remain CREST. Without it, "the CSV adapter unblocks
	// every source system" quietly means "every source system that agrees to
	// rename its columns to ours first" — which is the opposite of unblocking,
	// and it is the constraint that would have shaped the first partner
	// conversation.
	Mapping Mapping
}

// Mapping translates one source system's file into the canonical field set.
//
// Two mechanisms, because real exports need both. A DHIS2 event export names
// its date column `eventDate` and has no column at all for the unit of measure
// — that is not missing data, it is a fact about the programme rather than
// about the row, and it belongs in configuration next to the rest of what this
// deployment knows about this source.
type Mapping struct {
	// Columns maps a canonical field name to the column that carries it in
	// this source's files: {"period_start": "eventDate"}.
	Columns map[string]string `json:"columns,omitempty"`

	// Enrichment renames the columns CREST does not interpret but a definition
	// might: {"household_id": "Household ID"}.
	//
	// Needed because a tier map names the fields it requires, and it names them
	// in the deployment's vocabulary rather than the source's. Without this, a
	// source that calls it "Household ID" produces records that silently miss
	// the tier rule requiring `household_id` — a worker paid at a lower tier
	// for a reason having nothing to do with the quality of the evidence, and
	// nothing anywhere saying so. The PoC found exactly this.
	Enrichment map[string]string `json:"enrichment,omitempty"`

	// Constants supply canonical fields the file does not carry at all:
	// {"outcome_unit": "bednets-distributed"}.
	//
	// A column always wins over a constant. A constant is what this deployment
	// knows about every row from this source; a column is what the row itself
	// says, and the row is closer to the work.
	Constants map[string]string `json:"constants,omitempty"`
}

// Resolve answers "which raw value is this canonical field", given a lookup
// into the current row. It returns the value and whether anything supplied it.
//
// Precedence: a mapped column, then the canonical column name itself, then a
// constant. The middle step is what keeps every file that already speaks
// CREST's vocabulary working with no mapping configured at all.
// Column names match case-insensitively, because a mapping is written by a
// person reading a header — "Bednets distributed" — while the header itself is
// indexed lowercased. Requiring the two to agree on capitalisation would make
// the failure "nothing supplies outcome_value" for a column plainly present in
// the file, which is a bad hour for whoever is configuring a real source.
func (m Mapping) Resolve(field string, column func(string) (string, bool)) (string, bool) {
	if mapped, ok := m.Columns[field]; ok {
		if v, present := column(strings.ToLower(strings.TrimSpace(mapped))); present {
			return v, true
		}
	}
	if v, present := column(field); present {
		return v, true
	}
	if v, ok := m.Constants[field]; ok {
		return v, true
	}
	return "", false
}

// Mapped reports whether a raw column is consumed by the mapping, so the
// adapter knows not to also keep it as enrichment. A column that arrives as
// `eventDate` and leaves as `period_start` must not also be filed as an
// unrecognised extra — that is the same fact twice, and a tier map reading the
// second copy would be reading something CREST already interpreted.
func (m Mapping) Mapped(column string) bool {
	for _, mapped := range m.Columns {
		if strings.EqualFold(strings.TrimSpace(mapped), column) {
			return true
		}
	}
	return false
}

// EnrichmentName is the key an unrecognised column should be filed under: the
// deployment's name for it where one is configured, and the column's own name
// otherwise.
func (m Mapping) EnrichmentName(column string) string {
	for canonical, mapped := range m.Enrichment {
		if strings.EqualFold(strings.TrimSpace(mapped), column) {
			return canonical
		}
	}
	return column
}

// Row is one parsed record, with enough of its origin to explain itself.
type Row struct {
	Record schema.CanonicalWorkEvidenceRecord

	// Ref locates the row in the original file — "row 42". A rejection a person
	// cannot find in the file they sent is a rejection they cannot act on.
	Ref string
}

// Rejection is a row the adapter could not turn into a canonical record.
//
// Rejections are returned, not logged and dropped. A batch that silently loses
// three rows is a batch that pays three people nothing and tells no one.
type Rejection struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// Adapter translates one source system class into canonical records.
type Adapter interface {
	// Ref identifies the adapter and its version, e.g. "csv-batch@1". It ends
	// up in every record's provenance, and the adapter registry records which
	// versions are recognised — so a verifier can walk evidence back to a known
	// translator (§8).
	Ref() string

	// Parse reads a source payload. receivedAt is passed rather than read so
	// that a harness running a seven-day window in milliseconds produces
	// records whose timestamps agree with the rest of the run.
	Parse(r io.Reader, src Source, receivedAt time.Time) ([]Row, []Rejection, error)
}
