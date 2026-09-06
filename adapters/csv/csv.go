// Package csv is the batch-file adapter: one adapter class covering every
// source system that can produce a delimited export (§8).
//
// It is the lowest-common-denominator transport, and the one a programme with
// no API can always use. That is why the tier map's floor exists: a record that
// arrives this way, with only the mandatory core, is still valid Tier-1-capable
// evidence.
package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/pkg/schema"
)

// Version is the adapter's version, and it appears in every record's
// provenance. Bump it when the parsing changes, because a verifier resolving
// "which translator produced this" needs the answer to mean something.
const Version = "csv-batch@1"

// The columns the adapter understands. Everything else in the header becomes
// enrichment, kept verbatim — a source system's extra column is information,
// and discarding it because CREST does not recognise it is how a definition's
// tier map loses the field it needed.
const (
	colActivity    = "activity"
	colOutcome     = "outcome_value"
	colOutcomeUnit = "outcome_unit"
	colWorkerKind  = "worker_id_kind"
	colWorkerValue = "worker_id"
	colStart       = "period_start"
	colEnd         = "period_end"
	colGeography   = "geography"
	colRecordRef   = "source_record_ref"
)

// Adapter is the batch-file adapter.
type Adapter struct{}

// Plugin is the statically linked catalogue entry used by evidence service
// wiring. Adding a future adapter means adding its package plugin there; the
// intake route never constructs an adapter from request input.
func Plugin() adapters.Plugin {
	return adapters.Plugin{Ref: Version, New: func() adapters.Adapter { return Adapter{} }}
}

// Ref returns the adapter reference recorded in provenance.
func (Adapter) Ref() string { return Version }

// Parse reads a CSV and returns canonical records plus the rows it refused.
//
// A row is either a record or a rejection; nothing is dropped in between.
func (a Adapter) Parse(r io.Reader, src adapters.Source, receivedAt time.Time) ([]adapters.Row, []adapters.Rejection, error) {
	if !src.ValidProvenance() {
		return nil, nil, fmt.Errorf("source provenance is incomplete or unsupported; use registered deployment configuration")
	}
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	// Variable field counts are a malformed file, not a shape to accommodate:
	// a short row means a column was dropped, and guessing which one is how a
	// worker's outcome becomes someone else's.
	reader.FieldsPerRecord = 0

	header, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	index := map[string]int{}
	for i, name := range header {
		index[strings.TrimSpace(strings.ToLower(name))] = i
	}
	// Mandatory-core check, asked through the mapping rather than of the raw
	// header. A source that calls its date column `eventDate` is not missing a
	// date, and refusing its file for that reason is how "unblocks every source
	// system" turns into "rename your columns first".
	for _, required := range []string{colActivity, colOutcome, colOutcomeUnit, colWorkerValue, colStart} {
		if _, ok := src.Mapping.Resolve(required, func(name string) (string, bool) {
			_, present := index[name]
			return "", present
		}); !ok {
			return nil, nil, fmt.Errorf("nothing supplies %q: the file has no such column, the "+
				"source's mapping names none, and no constant is configured. The mandatory core "+
				"of a work-evidence record cannot be assembled without it (§8)", required)
		}
	}

	var rows []adapters.Row
	var rejected []adapters.Rejection
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// The line comes from the parse error rather than from a counter.
			// A quoted field containing a newline is one record over two
			// physical lines, and a counter drifts from that point on — which
			// breaks the one property the reference exists for: that a person
			// can find the row in the file they sent.
			ref := "row ?"
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				ref = fmt.Sprintf("row %d", parseErr.Line)
			}
			// A structurally broken row does not stop the batch. The other rows
			// describe work that happened, and refusing all of them because one
			// is malformed leaves people unpaid for someone else's typo.
			rejected = append(rejected, adapters.Rejection{Ref: ref, Reason: err.Error()})
			continue
		}

		// FieldPos reports where the record actually started, so this is the
		// line a person will find when they open the file.
		startLine, _ := reader.FieldPos(0)
		ref := fmt.Sprintf("row %d", startLine)

		row, reason := a.row(header, index, record, src, receivedAt, ref)
		if reason != "" {
			rejected = append(rejected, adapters.Rejection{Ref: ref, Reason: reason})
			continue
		}
		rows = append(rows, row)
	}
	return rows, rejected, nil
}

func (a Adapter) row(header []string, index map[string]int, record []string,
	src adapters.Source, receivedAt time.Time, ref string) (adapters.Row, string) {
	column := func(name string) (string, bool) {
		i, ok := index[name]
		if !ok || i >= len(record) {
			return "", false
		}
		return strings.TrimSpace(record[i]), true
	}
	get := func(field string) string {
		v, _ := src.Mapping.Resolve(field, column)
		return v
	}

	value, err := strconv.ParseFloat(get(colOutcome), 64)
	if err != nil {
		return adapters.Row{}, fmt.Sprintf("%s is not a number: %q", colOutcome, get(colOutcome))
	}
	if value < 0 {
		return adapters.Row{}, fmt.Sprintf("%s is negative", colOutcome)
	}
	start, err := parseTime(get(colStart))
	if err != nil {
		return adapters.Row{}, fmt.Sprintf("%s: %v", colStart, err)
	}
	period := schema.Period{Start: start}
	if s := get(colEnd); s != "" {
		end, err := parseTime(s)
		if err != nil {
			return adapters.Row{}, fmt.Sprintf("%s: %v", colEnd, err)
		}
		if end.Before(start) {
			return adapters.Row{}, "period ends before it starts"
		}
		period.End = &end
	}

	kind := schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKind(get(colWorkerKind))
	if kind == "" {
		kind = schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone
	}
	if get(colWorkerValue) == "" {
		return adapters.Row{}, "no worker identifier"
	}

	// Everything the adapter does not recognise, kept verbatim.
	enrichment := map[string]any{}
	known := map[string]bool{
		colActivity: true, colOutcome: true, colOutcomeUnit: true, colWorkerKind: true,
		colWorkerValue: true, colStart: true, colEnd: true, colGeography: true, colRecordRef: true,
	}
	for i, name := range header {
		key := strings.TrimSpace(strings.ToLower(name))
		if sensitiveField(key) && !known[key] {
			if i < len(record) && strings.TrimSpace(record[i]) != "" {
				return adapters.Row{}, fmt.Sprintf("enrichment field %q contains personal or precise-location data", key)
			}
		}
		// A column the mapping consumed is not an extra. Keeping it would file
		// the same fact twice — once interpreted, once raw — and a tier map
		// reading the raw copy would be reading around the adapter.
		if known[key] || src.Mapping.Mapped(key) || i >= len(record) {
			continue
		}
		if v := strings.TrimSpace(record[i]); v != "" {
			if sensitiveField(key) {
				return adapters.Row{}, fmt.Sprintf("enrichment field %q contains personal or precise-location data", key)
			}
			// Filed under the deployment's name for it where one is
			// configured. A definition's tier map names the fields it
			// requires, and it names them in the deployment's vocabulary.
			enrichment[src.Mapping.EnrichmentName(key)] = v
		}
	}

	rec := schema.CanonicalWorkEvidenceRecord{
		Activity: get(colActivity),
		Outcome:  schema.Outcome{Value: value, Unit: get(colOutcomeUnit)},
		WorkerJoiningIdentifier: schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier{
			Kind:  kind,
			Value: get(colWorkerValue),
		},
		Period: period,
		// Provenance comes from the deployment's configuration for this source,
		// never from the file. A CSV that carries a "source_class" column gets
		// it treated as enrichment like any other unrecognised field — which is
		// the whole point of §8's rule.
		Provenance: schema.Provenance{
			SourceClass:    src.Class,
			SystemRef:      &src.SystemRef,
			CaptureMethod:  src.CaptureMethod,
			SourceExposure: src.Exposure,
			AdapterRef:     a.Ref(),
			ReceivedAt:     receivedAt,
		},
	}
	if g := get(colGeography); g != "" {
		if looksLikePreciseLocation(g) {
			return adapters.Row{}, "geography contains a precise location; only a coarse geography may be stored"
		}
		rec.Geography = &g
	}
	if r := get(colRecordRef); r != "" {
		rec.Provenance.SourceRecordRef = &r
	}
	if len(enrichment) > 0 {
		rec.Enrichment = enrichment
	}
	return adapters.Row{Record: rec, Ref: ref}, ""
}

// Sensitive values are rejected before they can enter a canonical record. A
// source may carry these fields for its own purposes, but the evidence ledger
// has no reason to retain them.
func sensitiveField(name string) bool {
	n := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(name))
	switch n {
	case "lat", "latitude", "lon", "lng", "longitude", "gps", "geolocation", "coordinates",
		"name", "fullname", "firstname", "lastname", "email", "emailaddress", "address", "streetaddress",
		"workername", "legalname", "nationalid", "nationalidentifier", "governmentid", "phone", "phonenumber", "mobile", "contactnumber":
		return true
	default:
		return strings.HasSuffix(n, "email") || strings.HasSuffix(n, "phone") || strings.HasSuffix(n, "address") ||
			strings.HasSuffix(n, "latitude") || strings.HasSuffix(n, "longitude") ||
			strings.Contains(n, "coordinate") || strings.Contains(n, "geolocation")
	}
}

func looksLikePreciseLocation(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err != nil {
			return false
		}
	}
	return true
}

// parseTime accepts a date or a full timestamp. Source systems export both, and
// refusing a date would refuse most real files.
func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is neither a date nor an RFC3339 timestamp", s)
}
