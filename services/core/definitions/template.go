package definitions

// Template generation (p3_23): a spreadsheet template derived from one
// definition version, never stored.
//
// Derived, because a stored template can drift from the version it claims to
// serve. The columns come from the version's platform face plus the canonical
// evidence contract, so publishing v(n+1) inherently generates a fresh
// template and files built on the old one describe the old version — which is
// exactly the p3_23 callout: the template is tied to this version.

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// canonicalColumns is the evidence contract's own field set, in the order the
// CSV adapter documents them. These exist for every definition; the
// definition adds its required enrichment fields after them.
var canonicalColumns = []string{
	"activity",
	"worker_id_kind",
	"worker_id",
	"period_start",
	"period_end",
	"outcome_value",
	"outcome_unit",
	"geography",
	"source_record_ref",
}

// Template is what a source owner downloads: the columns a file for this
// definition version must carry, pinned to the version they serve.
type Template struct {
	DefinitionID string   `json:"definitionId"`
	Version      int      `json:"version"`
	Activity     string   `json:"activity"`
	Filename     string   `json:"filename"`
	Columns      []string `json:"columns"`
	// RequiredEnrichment names which columns beyond the canonical set the
	// version's tier map and platform face demand, so a source owner can see
	// why each column is there.
	RequiredEnrichment []string `json:"requiredEnrichment"`
}

// templateFor derives the template from one version. Pure, so a test can
// assert the version-pinning without a server.
func templateFor(d schema.Definition) Template {
	seen := map[string]bool{}
	for _, c := range canonicalColumns {
		seen[c] = true
	}
	extra := map[string]bool{}
	for _, f := range d.Faces.Platform.RequiredFields {
		if !seen[f] {
			extra[f] = true
		}
	}
	for _, rule := range d.TierMap {
		for _, f := range rule.RequiresFields {
			if !seen[f] {
				extra[f] = true
			}
		}
	}
	enrichment := make([]string, 0, len(extra))
	for f := range extra {
		enrichment = append(enrichment, f)
	}
	sort.Strings(enrichment)

	return Template{
		DefinitionID:       d.ID,
		Version:            d.Version,
		Activity:           d.Activity.Code,
		Filename:           fmt.Sprintf("%s-v%d-template.csv", strings.ReplaceAll(d.Activity.Code, ":", "-"), d.Version),
		Columns:            append(append([]string{}, canonicalColumns...), enrichment...),
		RequiredEnrichment: enrichment,
	}
}

// template serves GET /v1/definitions/{id}/versions/{version}/template.
// JSON by default; ?format=csv returns the header row itself, ready to save.
func (h *handlers) template(w http.ResponseWriter, r *http.Request) {
	version, ok := h.version(w, r)
	if !ok {
		return
	}
	def, err := getDefinition(r.Context(), h.d.DB.Q(), r.PathValue("id"), version)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "definition version", err, store.ErrNotFound)
		return
	}
	t := templateFor(def)
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", t.Filename))
		_, _ = fmt.Fprintln(w, strings.Join(t.Columns, ","))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, t)
}
