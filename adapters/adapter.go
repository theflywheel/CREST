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
