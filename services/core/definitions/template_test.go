package definitions

import (
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// The template is derived from the version, so two versions with different
// evidence rules produce different templates — p3_23's callout ("the template
// is tied to this version") as a property, not a caption.
func TestTemplateIsPinnedToTheVersion(t *testing.T) {
	doc := fullDraft()
	v1, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	if len(problems) != 0 {
		t.Fatal(problems)
	}

	doc.Sources.RequiredFields = []string{"household_id", "supervisor_id"}
	v2, problems := compile(doc, "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 2, "a", testTime)
	if len(problems) != 0 {
		t.Fatal(problems)
	}

	t1, t2 := templateFor(v1), templateFor(v2)
	if t1.Version != 1 || t2.Version != 2 {
		t.Fatalf("templates do not carry their versions: %d, %d", t1.Version, t2.Version)
	}
	if strings.Join(t1.Columns, ",") == strings.Join(t2.Columns, ",") {
		t.Fatal("two versions with different required fields produced the same template")
	}
	if !strings.Contains(t2.Filename, "v2") {
		t.Errorf("filename does not name the version: %s", t2.Filename)
	}
}

// Every canonical column is present, and the definition's extra requirements
// (platform face + tier map requiresFields) follow, deduplicated and sorted.
func TestTemplateColumns(t *testing.T) {
	def, problems := compile(fullDraft(), "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	tpl := templateFor(def)

	for _, canonical := range canonicalColumns {
		found := false
		for _, c := range tpl.Columns {
			if c == canonical {
				found = true
			}
		}
		if !found {
			t.Errorf("canonical column %q missing from template", canonical)
		}
	}
	// household_id appears in both the platform face and a tier rule; once.
	count := 0
	for _, c := range tpl.Columns {
		if c == "household_id" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("household_id appears %d times, want once", count)
	}
	if len(tpl.RequiredEnrichment) != 1 || tpl.RequiredEnrichment[0] != "household_id" {
		t.Errorf("requiredEnrichment = %v, want [household_id]", tpl.RequiredEnrichment)
	}
}

// requiredFieldsOf and presentAndMissing are the dry run's judgement about a
// record; a field carried in enrichment counts, a field nowhere does not.
func TestPresentAndMissing(t *testing.T) {
	def, problems := compile(fullDraft(), "crest:definition:01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "a", testTime)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	required := requiredFieldsOf(def)
	if len(required) != 1 || required[0] != "household_id" {
		t.Fatalf("requiredFieldsOf = %v, want [household_id]", required)
	}

	rec := schema.CanonicalWorkEvidenceRecord{
		Activity:   "chw.household.visit",
		Enrichment: map[string]any{"household_id": "H-42"},
	}
	present, missing := presentAndMissing(rec, required)
	if len(present) != 1 || len(missing) != 0 {
		t.Errorf("with enrichment: present=%v missing=%v", present, missing)
	}

	rec.Enrichment = nil
	present, missing = presentAndMissing(rec, required)
	if len(present) != 0 || len(missing) != 1 {
		t.Errorf("without enrichment: present=%v missing=%v", present, missing)
	}
}
