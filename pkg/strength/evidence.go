package strength

import (
	"fmt"
	"sort"
	"strings"

	"github.com/theflywheel/crest/pkg/schema"
)

// EvidenceFields uses the same vocabulary for intake, preview and credentials.
func EvidenceFields(rec schema.CanonicalWorkEvidenceRecord) []string {
	fields := []string{"activity", "outcome_value", "outcome_unit", "period_start"}
	if rec.Period.End != nil {
		fields = append(fields, "period_end")
	}
	if rec.WorkerJoiningIdentifier.Value != "" {
		fields = append(fields, "worker_id", "worker_id_kind")
	}
	if rec.Provenance.SourceRecordRef != nil && *rec.Provenance.SourceRecordRef != "" {
		fields = append(fields, "source_record_ref")
	}
	if rec.Geography != nil && *rec.Geography != "" {
		fields = append(fields, "geography")
	}
	for name, value := range rec.Enrichment {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}

// EvidenceFieldsForUnit projects a legacy stored unit into the non-sensitive
// field presence vocabulary used by canonical intake. Units created before
// presence persistence cannot prove whether a joining identifier was carried,
// so this fallback never invents worker identifier fields.
func EvidenceFieldsForUnit(unit schema.Unit) []string {
	rec := schema.CanonicalWorkEvidenceRecord{
		Activity:   "present",
		Enrichment: unit.Enrichment,
		Geography:  unit.Geography,
		Outcome:    unit.Outcome,
		Period:     unit.Period,
		Provenance: unit.Provenance,
	}
	return EvidenceFields(rec)
}

// EvaluateEvidence validates a canonical record against its pinned definition
// and registered source assessment.
func EvaluateEvidence(rec schema.CanonicalWorkEvidenceRecord, def schema.Definition, systemRef string,
	assurance schema.IdentityAssurance, assessment *SourceAssessment) (Result, error) {
	if def.Faces.Platform.SchemaRef == "" {
		return Result{}, fmt.Errorf("definition has no evidence schema")
	}
	if err := schema.Validate(def.Faces.Platform.SchemaRef, rec); err != nil {
		return Result{}, fmt.Errorf("definition evidence schema: %w", err)
	}
	if rec.Activity != def.Activity.Code || rec.Outcome.Unit != def.OutcomeUnit {
		return Result{}, fmt.Errorf("activity and outcome unit must match definition %s@%d", def.ID, def.Version)
	}
	allowed := false
	for _, source := range def.Faces.Platform.SourceSystems {
		if source == systemRef && systemRef != "" {
			allowed = true
		}
	}
	if !allowed {
		return Result{}, fmt.Errorf("source %q is not allowed by definition %s@%d", systemRef, def.ID, def.Version)
	}
	fields := EvidenceFields(rec)
	present := map[string]bool{}
	for _, field := range fields {
		present[field] = true
	}
	for _, required := range def.Faces.Platform.RequiredFields {
		if !present[required] {
			return Result{}, fmt.Errorf("definition requires evidence field %q", required)
		}
	}
	result := Evaluate(Facts{Provenance: rec.Provenance, PresentFields: fields, IdentityAssurance: assurance}, def, assessment)
	if !result.Acceptable {
		return result, fmt.Errorf("evidence is not acceptable: %s", strings.Join(result.Because, "; "))
	}
	return result, nil
}
