package definitions

// Dry run (p3_27): sample evidence against a draft, committing nothing.
//
// The sample runs through the same CSV adapter and the same strength function
// the real pipeline uses — not a simulation of them — so what the author sees
// is what ingestion would do. The one thing this endpoint must never do is
// write: no unit, no source registration, no queue entry. It reads the draft,
// compiles it in memory, and answers.
//
// Identity resolution is deliberately out of scope, and the response says so
// per row rather than pretending: the p3_27 callout's three unresolved
// workers are an identity-resolution fact this endpoint cannot decide,
// because resolving a joining identifier to a Party is the ingestion
// pipeline's job against real registries, not a draft's.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/theflywheel/crest/adapters"
	csvadapter "github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/pkg/strength"
)

// dryRunRequest carries the sample and how it should be read. Source facts
// come from the request, not the file — the adapter contract's rule that
// provenance is never source-asserted holds in rehearsal too.
type dryRunRequest struct {
	// CSV is the sample file, verbatim.
	CSV string `json:"csv"`
	// SourceClass and CaptureMethod are what the deployment would configure
	// for this source (p3_22): the structural ceiling on tier.
	SourceClass   schema.SourceClass   `json:"sourceClass"`
	CaptureMethod schema.CaptureMethod `json:"captureMethod"`
	SystemRef     string               `json:"systemRef,omitempty"`
	// Mapping is the draft mapping under test (p3_25).
	Mapping adapters.Mapping `json:"mapping"`
	// IdentityAssurance to assume for every row's worker, since a dry run
	// resolves nobody. Empty means IA-0, the honest floor.
	IdentityAssurance schema.IdentityAssurance `json:"identityAssurance,omitempty"`
}

type dryRunRow struct {
	Ref      string   `json:"ref"`
	Activity string   `json:"activity"`
	Matches  bool     `json:"matchesDefinition"`
	Tier     int      `json:"tier,omitempty"`
	Because  []string `json:"because,omitempty"`
	Missing  []string `json:"missingRequiredFields,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// dryRun serves POST /v1/definition-drafts/{id}/dry-run.
func (h *draftHandlers) dryRun(w http.ResponseWriter, r *http.Request) {
	var req dryRunRequest
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.CSV == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "no_sample", "a dry run needs sample records")
		return
	}
	if req.SourceClass == "" || req.CaptureMethod == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "no_source_facts",
			"sourceClass and captureMethod come from the deployment's knowledge of the source, and a dry run cannot guess them")
		return
	}

	draft, err := getDraft(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "draft", err, store.ErrNotFound)
		return
	}

	defID := draft.DefinitionID
	if defID == "" {
		// The same schema-valid placeholder validate uses: a dry run compiles
		// a draft that may have no definition id, and must not mint one.
		defID = previewDefinitionID
	}
	now := h.d.Clock.Now()
	compiled, problems := compile(draft.Doc, defID, draft.BaseVersion+1, draft.CreatedBy, now)
	// A dry run against a draft with open evidence rules would judge rows
	// against rules that do not exist yet; refuse those, allow the rest.
	for _, p := range problems {
		if p.Section == "evidence" || p.Section == "activity" {
			httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":    "draft_not_testable",
				"message":  "the evidence rules and activity must be settled before a dry run can judge records against them",
				"problems": problems,
			})
			return
		}
	}

	adapter := csvadapter.Adapter{}
	src := adapters.Source{
		Class:         req.SourceClass,
		CaptureMethod: req.CaptureMethod,
		// A dry run is by definition a person uploading a file by hand.
		Exposure:  schema.SourceExposureSupervisedUpload,
		SystemRef: req.SystemRef,
		Mapping:   req.Mapping,
	}
	rows, rejections, err := adapter.Parse(strings.NewReader(req.CSV), src, now)
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "unparseable", "the sample does not parse as CSV: %v", err)
		return
	}

	required := requiredFieldsOf(compiled)
	results := make([]dryRunRow, 0, len(rows))
	for _, row := range rows {
		res := dryRunRow{Ref: row.Ref, Activity: row.Record.Activity}
		if row.Record.Activity != compiled.Activity.Code {
			res.Problems = append(res.Problems,
				"activity "+row.Record.Activity+" is not this definition's "+compiled.Activity.Code)
		} else {
			res.Matches = true
		}
		present, missing := presentAndMissing(row.Record, required)
		res.Missing = missing
		verdict := strength.Evaluate(strength.Facts{
			Provenance:        row.Record.Provenance,
			PresentFields:     present,
			IdentityAssurance: req.IdentityAssurance,
		}, compiled, nil)
		if verdict.Acceptable {
			res.Tier = verdict.Tier
		}
		res.Because = verdict.Because
		results = append(results, res)
	}
	if rejections == nil {
		rejections = []adapters.Rejection{}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"definitionId": compiled.ID,
		"version":      compiled.Version,
		"rows":         results,
		"rejections":   rejections,
		"committed":    false,
		"note": "nothing was written: no unit, no source, no queue entry. Identity resolution is not rehearsed here — " +
			"joining identifiers resolve against real registries at ingestion, and unresolved workers there go to the unclear queue, never a silent guess",
	})
}

// requiredFieldsOf is every field the definition can demand of a record: the
// platform face's required fields plus each tier rule's requiresFields.
func requiredFieldsOf(d schema.Definition) []string {
	seen := map[string]bool{}
	for _, f := range d.Faces.Platform.RequiredFields {
		seen[f] = true
	}
	for _, rule := range d.TierMap {
		for _, f := range rule.RequiresFields {
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// presentAndMissing splits the definition's demandable fields by whether the
// record carries them — canonical fields by name, everything else in
// enrichment, the same way the pipeline computes present fields.
func presentAndMissing(rec schema.CanonicalWorkEvidenceRecord, required []string) (present, missing []string) {
	has := func(f string) bool {
		switch f {
		case "activity":
			return rec.Activity != ""
		case "period_start":
			return !rec.Period.Start.IsZero()
		case "period_end":
			return !rec.Period.End.IsZero()
		case "outcome_value":
			return true // Outcome is required by the record schema itself
		case "outcome_unit":
			return rec.Outcome.Unit != ""
		case "geography":
			return rec.Geography != nil && *rec.Geography != ""
		case "source_record_ref":
			return rec.Provenance.SourceRecordRef != nil && *rec.Provenance.SourceRecordRef != ""
		default:
			_, ok := rec.Enrichment[f]
			return ok
		}
	}
	for _, f := range required {
		if has(f) {
			present = append(present, f)
		} else {
			missing = append(missing, f)
		}
	}
	return present, missing
}
