package definitions

// Compilation from a mutable draft to an immutable definition version (P-3).
//
// A draft is the wizard's document: sections a person fills in over days, in
// any order, half-finished for most of its life. A definition version is the
// object credentials pin and verifiers resolve. Nothing may blur the two, so
// the translation is one pure function with no I/O: everything it refuses, it
// refuses by name, and everything it produces can be checked against the
// definition schema before a row exists anywhere.
//
// The layering test runs through every field here. What the primitive gets:
// activity, counting, the three faces, the tier map, attester functions —
// facts no two CREST deployments could shape differently. What stays L2:
// sector and category vocabulary (classification values), check intensity,
// validation posture — recorded as bounded strings in slots the
// infrastructure never interprets. What stays out of the definition entirely:
// everything about money. Tranches, preconditions, deductions and the rate
// handoff are trusted-payments LinkedRecords by reference, because "payment
// set-ups link by reference, never embed: a definition is complete and usable
// with no rate attached" (§7).

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// DraftDoc is the wizard document, one field per section. Every section is
// optional while the draft is open; compile says which are still open
// questions (p3_18) rather than refusing to look.
type DraftDoc struct {
	// Scope (p3_2, p3_4): sector and category. Pure L2 vocabulary — compiled
	// into `classification`, never interpreted.
	Scope *ScopeSection `json:"scope,omitempty"`

	// Activity and counting (p3_3, p3_5, p3_6, p3_7): what the work is and
	// how a completed instance is counted.
	Activity *ActivitySection `json:"activity,omitempty"`

	// Parties (p3_8): who performs, and which authorization functions may
	// attest evidence against this definition.
	Parties *PartiesSection `json:"parties,omitempty"`

	// Evidence (p3_9): what counts as proof, at which strength. The tier map
	// rules carry sourceClass and captureMethod — provenance vocabulary, the
	// tier-capping choice. No rule stores a tier on a record; the map is L2
	// data the L1 strength function reads at query time (§6).
	Evidence *EvidenceSection `json:"evidence,omitempty"`

	// Validation (p3_10): who validates and under what posture.
	Validation *ValidationSection `json:"validation,omitempty"`

	// Sources (p3_22–p3_26): where evidence comes from. Connection details
	// are configuration references; CREST never sees a credential value, and
	// compile refuses anything that looks like one.
	Sources *SourcesSection `json:"sources,omitempty"`

	// Cascade (p3_21): this work's place in a training cascade. Compiled to a
	// linked-definition LinkedRecord, not a field on the primitive — a
	// use-case relationship belongs in the extension point (§2).
	Cascade *CascadeSection `json:"cascade,omitempty"`

	// Extensions (p3_14): what the form did not have.
	Extensions map[string]ExtensionField `json:"extensions,omitempty"`

	// Payment (p3_11, p3_12, p3_13, p3_20): structure intent — tranches,
	// preconditions, deductions, roles. Kept on the draft and carried into
	// the payment-structure LinkedRecord at submit; never a definition field.
	Payment *PaymentSection `json:"payment,omitempty"`
}

// ScopeSection is p3_2 and p3_4: sector and category, pure L2 vocabulary.
type ScopeSection struct {
	Sector   string `json:"sector,omitempty"`
	Category string `json:"category,omitempty"`
}

// ActivitySection is the activity and its counting (p3_3, p3_5, p3_6, p3_7).
type ActivitySection struct {
	Code        string                     `json:"code,omitempty"`
	Label       string                     `json:"label,omitempty"`
	SkillCode   string                     `json:"skillCode,omitempty"`
	OutcomeUnit string                     `json:"outcomeUnit,omitempty"`
	Counting    *schema.DefinitionCounting `json:"counting,omitempty"`
}

// PartiesSection is p3_8: who performs, and who may attest.
type PartiesSection struct {
	PerformerRole string `json:"performerRole,omitempty"`
	PartyType     string `json:"partyType,omitempty"`
	// AttesterFunctions become authorisedAttesterFunctions: an Authorization
	// must carry one of these to submit evidence.
	AttesterFunctions []string `json:"attesterFunctions,omitempty"`
}

// EvidenceSection is p3_9: what counts as proof, at which strength.
type EvidenceSection struct {
	Summary                 string                         `json:"summary,omitempty"`
	EvidenceInPlainLanguage []string                       `json:"evidenceInPlainLanguage,omitempty"`
	TierCeiling             int                            `json:"tierCeiling,omitempty"`
	CheckIntensity          string                         `json:"checkIntensity,omitempty"`
	TierMap                 []schema.DefinitionTierMapItem `json:"tierMap,omitempty"`
}

// ValidationSection is p3_10: who validates, under what posture.
type ValidationSection struct {
	AuthorisedIssuers []string `json:"authorisedIssuers,omitempty"`
	SpecifierPartyID  string   `json:"specifierPartyId,omitempty"`
	// Posture and DelayDays are programme policy — classification strings,
	// nothing in the infrastructure reads them.
	Posture   string `json:"posture,omitempty"`
	DelayDays *int   `json:"delayDays,omitempty"`
}

// SourcesSection is p3_22 to p3_26: where evidence comes from.
type SourcesSection struct {
	SourceSystems  []string           `json:"sourceSystems,omitempty"`
	RequiredFields []string           `json:"requiredFields,omitempty"`
	SchemaRef      string             `json:"schemaRef,omitempty"`
	Connections    []SourceConnection `json:"connections,omitempty"`
}

// SourceConnection describes how one source system will reach this
// deployment. It is a description, not a credential store: the definition can
// be reviewed, signed and published with the connection described but not yet
// credentialled (p3_26) — the adaptor simply will not run until the platform
// team supplies the secret, under the name credentialRef points at.
type SourceConnection struct {
	SystemRef  string `json:"systemRef"`
	AdapterRef string `json:"adapterRef,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	// CredentialRef names where the platform team keeps the secret (a vault
	// path, an env var name). Never the secret itself.
	CredentialRef string            `json:"credentialRef,omitempty"`
	Mapping       map[string]any    `json:"mapping,omitempty"`
	Settings      map[string]string `json:"settings,omitempty"`
}

// CascadeSection is p3_21: this work's place in a training cascade.
type CascadeSection struct {
	RoleLevel             string `json:"roleLevel,omitempty"`
	TrainedByDefinitionID string `json:"trainedByDefinitionId,omitempty"`
	TrainedByVersion      int    `json:"trainedByVersion,omitempty"`
}

// ExtensionField is one p3_14 escape-hatch field: label, declared type, value as text.
type ExtensionField struct {
	Label     string `json:"label"`
	ValueType string `json:"valueType"`
	Value     string `json:"value"`
}

// PaymentSection is p3_11 to p3_13 and p3_20: payment structure intent, never price.
type PaymentSection struct {
	// Roles (p3_11, p3_20): who sets the rate, who sets the mechanism, who
	// validates, who authored. Role names or party ids — descriptive; the
	// authority itself is the payments service's RateOwnerAssignment (F-1).
	Roles map[string]string `json:"roles,omitempty"`
	// Tranches (p3_12): the stacked pay structure. Shares of a rate somebody
	// else will publish, never amounts of money — pricing is the rate
	// owner's, and this section cannot preempt it.
	Tranches      []Tranche   `json:"tranches,omitempty"`
	Preconditions []string    `json:"preconditions,omitempty"`
	Deductions    []Deduction `json:"deductions,omitempty"`
}

// Tranche is one slice of the stacked pay structure (p3_12).
type Tranche struct {
	Label     string `json:"label"`
	Share     string `json:"share,omitempty"`
	Condition string `json:"condition,omitempty"`
}

// Deduction is one named reduction rule (p3_13).
type Deduction struct {
	Label string `json:"label"`
	Rule  string `json:"rule"`
}

// Problem is one thing standing between a draft and a definition, named by
// the section a person would go back to. p3_18 renders exactly this list.
type Problem struct {
	Section string `json:"section"`
	Field   string `json:"field,omitempty"`
	Reason  string `json:"reason"`
}

// secretKey matches connection setting keys that smell like a secret. The
// check is deliberately broad: a false positive costs a rename, a false
// negative persists a credential CREST promised never to see.
var secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[-_]?key|private[-_]?key|credential($|s)|client[-_]?secret|bearer|auth)`)

// checkConnections refuses connection details that carry secret material.
// The reference's own framing (p3_26): nothing on that screen is a secret,
// and this is the function that keeps the claim true rather than aspirational.
func checkConnections(s *SourcesSection) []Problem {
	if s == nil {
		return nil
	}
	var out []Problem
	for i, c := range s.Connections {
		where := fmt.Sprintf("connections[%d]", i)
		for k := range c.Settings {
			if secretKey.MatchString(k) {
				out = append(out, Problem{Section: "sources", Field: where + "." + k,
					Reason: "looks like a secret; CREST stores a credentialRef naming where the platform team keeps it, never the value"})
			}
		}
		if c.CredentialRef != "" && (strings.ContainsAny(c.CredentialRef, " \n") || len(c.CredentialRef) > 256) {
			out = append(out, Problem{Section: "sources", Field: where + ".credentialRef",
				Reason: "credentialRef is a reference — a vault path or variable name — not credential material"})
		}
	}
	return out
}

// checkExtensions verifies each extension value reads as its declared type.
// A typed escape hatch that does not check its types is an untyped one.
func checkExtensions(ext map[string]ExtensionField) []Problem {
	var out []Problem
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := ext[k]
		var bad bool
		switch f.ValueType {
		case "string":
		case "number":
			_, err := strconv.ParseFloat(f.Value, 64)
			bad = err != nil
		case "boolean":
			bad = f.Value != "true" && f.Value != "false"
		case "date":
			_, err := time.Parse("2006-01-02", f.Value)
			bad = err != nil
		default:
			out = append(out, Problem{Section: "extensions", Field: k,
				Reason: fmt.Sprintf("valueType %q is not one of string, number, boolean, date", f.ValueType)})
			continue
		}
		if bad {
			out = append(out, Problem{Section: "extensions", Field: k,
				Reason: fmt.Sprintf("value %q does not read as a %s", f.Value, f.ValueType)})
		}
	}
	return out
}

// compile turns a draft into the definition version it would become, plus
// every problem in the way. It is total: a half-empty draft compiles to a
// half-empty definition and a full problem list, which is what the review
// screen needs — refusal would leave p3_18 with nothing to show.
//
// specifiedAt and authoredBy are parameters, not lookups: this function does
// no I/O, so a test drives the clock rather than sleeping on one.
func compile(doc DraftDoc, defID string, version int, authoredBy string, at time.Time) (schema.Definition, []Problem) {
	var problems []Problem
	need := func(section, field, what string) {
		problems = append(problems, Problem{Section: section, Field: field, Reason: what + " is still open"})
	}

	d := schema.Definition{
		ID:                defID,
		Version:           version,
		State:             schema.DefinitionStateDRAFT,
		AuthoredByPartyID: authoredBy,
		CreatedAt:         at,
	}

	// Activity and counting.
	if a := doc.Activity; a != nil {
		d.Activity = schema.DefinitionActivity{Code: a.Code, Label: a.Label}
		d.OutcomeUnit = a.OutcomeUnit
		d.Counting = a.Counting
		if a.SkillCode != "" {
			sc := a.SkillCode
			d.SkillCode = &sc
		}
		if a.Code == "" {
			need("activity", "code", "the activity code")
		}
		if a.Label == "" {
			need("activity", "label", "the activity label")
		}
		if a.OutcomeUnit == "" {
			need("activity", "outcomeUnit", "the unit of work")
		}
		if a.Counting == nil {
			need("activity", "counting.basis", "how this work is counted")
		} else if a.Counting.Basis == schema.DefinitionCountingBasisOutcome && a.Counting.Outcome == nil {
			need("activity", "counting.outcome", "the outcome indicator")
		}
	} else {
		need("activity", "", "the activity section")
	}

	// Classification: L2 vocabulary in the L1 slot. Values only — which keys
	// a deployment uses is its own business.
	class := map[string]any{}
	put := func(k, v string) {
		if v != "" {
			class[k] = v
		}
	}
	if doc.Scope != nil {
		put("sector", doc.Scope.Sector)
		put("category", doc.Scope.Category)
	} else {
		need("scope", "sector", "the sector")
	}
	if doc.Parties != nil {
		put("performerRole", doc.Parties.PerformerRole)
		put("performerPartyType", doc.Parties.PartyType)
	}
	if doc.Evidence != nil {
		put("checkIntensity", doc.Evidence.CheckIntensity)
	}
	if doc.Validation != nil {
		put("validationPosture", doc.Validation.Posture)
		if doc.Validation.DelayDays != nil {
			put("validationDelayDays", strconv.Itoa(*doc.Validation.DelayDays))
		}
	}
	if len(class) > 0 {
		d.Classification = class
	}

	// Attesters.
	if doc.Parties == nil || len(doc.Parties.AttesterFunctions) == 0 {
		need("parties", "attesterFunctions", "who may attest evidence")
	} else {
		d.AuthorisedAttesterFunctions = doc.Parties.AttesterFunctions
	}

	// Worker face and tier map.
	if e := doc.Evidence; e != nil {
		d.Faces.Worker = schema.DefinitionFacesWorker{
			Summary:                 e.Summary,
			EvidenceInPlainLanguage: e.EvidenceInPlainLanguage,
			TierCeiling:             e.TierCeiling,
		}
		d.TierMap = e.TierMap
		if e.Summary == "" {
			need("evidence", "summary", "the plain-language summary")
		}
		if len(e.EvidenceInPlainLanguage) == 0 {
			need("evidence", "evidenceInPlainLanguage", "what the worker will see as proof")
		}
		if e.TierCeiling < 1 || e.TierCeiling > 3 {
			need("evidence", "tierCeiling", "the tier ceiling (1–3)")
		}
		if len(e.TierMap) == 0 {
			need("evidence", "tierMap", "at least one evidence-to-tier rule")
		}
		for i, rule := range e.TierMap {
			if e.TierCeiling >= 1 && rule.Tier > e.TierCeiling {
				problems = append(problems, Problem{Section: "evidence",
					Field:  fmt.Sprintf("tierMap[%d]", i),
					Reason: fmt.Sprintf("rule grants tier %d above the ceiling %d the worker face promises; the two faces are one record and may not disagree", rule.Tier, e.TierCeiling)})
			}
		}
	} else {
		need("evidence", "", "the evidence section")
	}

	// Platform face.
	if s := doc.Sources; s != nil {
		d.Faces.Platform = schema.DefinitionFacesPlatform{
			SourceSystems:  s.SourceSystems,
			RequiredFields: s.RequiredFields,
			SchemaRef:      s.SchemaRef,
		}
		if len(s.SourceSystems) == 0 {
			need("sources", "sourceSystems", "where evidence comes from")
		}
		if len(s.RequiredFields) == 0 {
			need("sources", "requiredFields", "which fields a record must carry")
		}
		if s.SchemaRef == "" {
			d.Faces.Platform.SchemaRef = schema.IDEvidenceRecord
		}
	} else {
		need("sources", "", "the evidence source section")
	}

	// Verifier face. specifiedAt is stamped here; the signature itself is the
	// ratification act, recorded in the event log with its actor.
	if v := doc.Validation; v != nil {
		d.Faces.Verifier = schema.DefinitionFacesVerifier{
			AuthorisedIssuers: v.AuthorisedIssuers,
			SpecifierPartyID:  v.SpecifierPartyID,
			SpecifiedAt:       at,
		}
		if len(v.AuthorisedIssuers) == 0 {
			need("validation", "authorisedIssuers", "who a verifier should accept issuance from")
		}
		if v.SpecifierPartyID == "" {
			d.Faces.Verifier.SpecifierPartyID = authoredBy
		}
	} else {
		need("validation", "", "the validation section")
	}

	if len(doc.Extensions) > 0 {
		ext := map[string]any{}
		for k, f := range doc.Extensions {
			ext[k] = map[string]any{"label": f.Label, "valueType": f.ValueType, "value": f.Value}
		}
		d.Extensions = ext
	}

	problems = append(problems, checkExtensions(doc.Extensions)...)
	problems = append(problems, checkConnections(doc.Sources)...)

	// Cascade sanity: a definition cannot be trained by itself.
	if c := doc.Cascade; c != nil && c.TrainedByDefinitionID == defID && defID != "" {
		problems = append(problems, Problem{Section: "cascade", Field: "trainedByDefinitionId",
			Reason: "a definition cannot be its own training prerequisite"})
	}

	return d, problems
}

// applyRatification is the pure half of the ratify transition: separation of
// duties, and the pending-fields declaration (p3_15). Only the ratifier names
// what stays pending — it is their judgement being recorded, and recording it
// under the author's hand would put the approver's name on a list they never
// saw.
func applyRatification(d *schema.Definition, ratifier string, pending []string) error {
	if ratifier == d.AuthoredByPartyID {
		return ErrSelfRatified
	}
	d.RatifiedByPartyID = &ratifier
	if len(pending) > 0 {
		seen := map[string]bool{}
		fields := make([]string, 0, len(pending))
		for _, f := range pending {
			f = strings.TrimSpace(f)
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			fields = append(fields, f)
		}
		d.PendingFields = fields
	}
	return nil
}
