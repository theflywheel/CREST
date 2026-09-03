package definitions

// The authoring endpoints (P-3): a mutable draft over the immutable
// definitions table, and the submit that turns one into the other.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

type draftHandlers struct{ d service.Deps }

// createDraft starts a draft — empty (p3_2, "Define new work") or cloned
// from an existing definition version (p3_1, "Clone a version").
//
// Cloning reads the version and rebuilds the wizard document from it, so the
// author edits a copy: nothing on this path can reach the source version,
// which stays exactly as ratified.
func (h *draftHandlers) createDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CreatedByPartyID      string    `json:"createdByPartyId"`
		CloneFromDefinitionID string    `json:"cloneFromDefinitionId,omitempty"`
		CloneFromVersion      int       `json:"cloneFromVersion,omitempty"`
		Doc                   *DraftDoc `json:"doc,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.CreatedByPartyID == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "no_author",
			"a draft records who is authoring it")
		return
	}

	now := h.d.Clock.Now()
	draft := Draft{
		ID:        id.New(h.d.Clock, "definition-draft"),
		State:     draftOpen,
		CreatedBy: body.CreatedByPartyID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if body.Doc != nil {
		draft.Doc = *body.Doc
	}

	if body.CloneFromDefinitionID != "" {
		src, err := getDefinition(r.Context(), h.d.DB.Q(), body.CloneFromDefinitionID, body.CloneFromVersion)
		if err != nil {
			httpx.NotFoundOr(w, h.d.Log, "definition to clone", err, store.ErrNotFound)
			return
		}
		draft.DefinitionID = src.ID
		draft.BaseVersion = src.Version
		draft.Doc = docFromDefinition(src)
	}

	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertDraft(r.Context(), tx, draft)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "create draft", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, draft)
}

func (h *draftHandlers) getDraft(w http.ResponseWriter, r *http.Request) {
	draft, err := getDraft(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "draft", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, draft)
}

func (h *draftHandlers) listDrafts(w http.ResponseWriter, r *http.Request) {
	drafts, err := listDrafts(r.Context(), h.d.DB.Q(), r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list drafts", err)
		return
	}
	if drafts == nil {
		drafts = []Draft{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"drafts": drafts})
}

// putSection replaces one wizard section. Whole-section writes, because that
// is what the wizard's Continue buttons mean: the screen's state, as left.
func (h *draftHandlers) putSection(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !httpx.ReadJSON(w, r, &raw) {
		return
	}
	section := r.PathValue("section")

	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		draft, err := getDraft(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if draft.State != draftOpen {
			return ErrDraftClosed
		}
		if err := setSection(&draft.Doc, section, raw); err != nil {
			return err
		}
		return updateDraftDoc(r.Context(), tx, draft.ID, draft.Doc, h.d.Clock.Now())
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such draft")
	case errors.Is(err, ErrDraftClosed):
		httpx.WriteError(w, http.StatusConflict, "draft_closed", "%s", ErrDraftClosed)
	case errors.As(err, new(*sectionError)):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "bad_section", "%v", err)
	case err != nil:
		httpx.Fail(w, h.d.Log, "update draft section", err)
	default:
		draft, err := getDraft(r.Context(), h.d.DB.Q(), r.PathValue("id"))
		if err != nil {
			httpx.Fail(w, h.d.Log, "reread draft", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, draft)
	}
}

func (h *draftHandlers) discard(w http.ResponseWriter, r *http.Request) {
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return closeDraft(r.Context(), tx, r.PathValue("id"), draftDiscarded, 0, h.d.Clock.Now())
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such draft")
	case errors.Is(err, ErrDraftClosed):
		httpx.WriteError(w, http.StatusConflict, "draft_closed", "%s", ErrDraftClosed)
	case err != nil:
		httpx.Fail(w, h.d.Log, "discard draft", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"state": draftDiscarded})
	}
}

// validate compiles the draft and reports what stands in the way, without
// writing anything. This is p3_15's review and p3_18's open-questions list:
// the same function submit runs, so the review can never disagree with the
// submission it precedes.
func (h *draftHandlers) validate(w http.ResponseWriter, r *http.Request) {
	draft, err := getDraft(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "draft", err, store.ErrNotFound)
		return
	}
	defID := draft.DefinitionID
	if defID == "" {
		defID = "crest:definition:PREVIEW"
	}
	compiled, problems := compile(draft.Doc, defID, draft.BaseVersion+1, draft.CreatedBy, h.d.Clock.Now())
	if len(problems) == 0 {
		if err := schema.Validate(schema.IDDefinition, compiled); err != nil {
			var ve *schema.ValidationError
			if errors.As(err, &ve) {
				for _, p := range ve.Problems {
					problems = append(problems, Problem{Section: "schema", Reason: p})
				}
			} else {
				httpx.Fail(w, h.d.Log, "validate draft", err)
				return
			}
		}
	}
	if problems == nil {
		problems = []Problem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ready":    len(problems) == 0,
		"problems": problems,
		"preview":  compiled,
	})
}

// submit compiles the draft into the next immutable definition version, in
// DRAFT state, awaiting ratification. In one transaction: the definitions
// row, the linked records the draft implies (cascade, payment structure),
// the SUBMITTED event, and the draft's closure — so a crash leaves either a
// draft still open or a version fully accounted for, never something between.
func (h *draftHandlers) submit(w http.ResponseWriter, r *http.Request) {
	var out schema.Definition
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		draft, err := getDraft(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if draft.State != draftOpen {
			return ErrDraftClosed
		}

		defID := draft.DefinitionID
		if defID == "" {
			defID = id.New(h.d.Clock, "definition")
		}
		version, err := nextVersion(r.Context(), tx, defID)
		if err != nil {
			return err
		}

		now := h.d.Clock.Now()
		compiled, problems := compile(draft.Doc, defID, version, draft.CreatedBy, now)
		if len(problems) > 0 {
			return &notReadyError{Problems: problems}
		}
		if err := schema.Validate(schema.IDDefinition, compiled); err != nil {
			return err
		}
		if err := insertDefinition(r.Context(), tx, compiled); err != nil {
			return err
		}
		if err := insertImpliedRecords(r.Context(), tx, h.d, draft.Doc, compiled, now); err != nil {
			return err
		}
		if err := appendEvent(r.Context(), tx, defID, version, eventSubmitted, draft.CreatedBy, now,
			map[string]any{"draftId": draft.ID}); err != nil {
			return err
		}
		if draft.DefinitionID == "" {
			if err := setDraftDefinition(r.Context(), tx, draft.ID, defID); err != nil {
				return err
			}
		}
		out = compiled
		return closeDraft(r.Context(), tx, draft.ID, draftSubmitted, version, now)
	})

	var notReady *notReadyError
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such draft")
	case errors.Is(err, ErrDraftClosed):
		httpx.WriteError(w, http.StatusConflict, "draft_closed", "%s", ErrDraftClosed)
	case errors.As(err, &notReady):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":    "not_ready",
			"message":  "the draft still has open questions; they are listed, not guessed at",
			"problems": notReady.Problems,
		})
	case err != nil:
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "submit draft", err)
	default:
		httpx.WriteJSON(w, http.StatusCreated, out)
	}
}

// insertImpliedRecords writes the LinkedRecords a draft's sections imply:
// the training cascade (p3_21) and the payment structure (p3_11–p3_13).
// LinkedRecords, not definition fields — the extension point exists so the
// core object never grows a use-case limb (§2), and payment shape in
// particular links by reference, never embeds (§7).
func insertImpliedRecords(ctx context.Context, tx store.Querier, d service.Deps,
	doc DraftDoc, def schema.Definition, now time.Time) error {
	if c := doc.Cascade; c != nil && (c.TrainedByDefinitionID != "" || c.RoleLevel != "") {
		payload := map[string]any{"definitionVersion": def.Version}
		if c.RoleLevel != "" {
			payload["roleLevel"] = c.RoleLevel
		}
		if c.TrainedByDefinitionID != "" {
			payload["relation"] = "trained-by"
			ref := map[string]any{"id": c.TrainedByDefinitionID}
			if c.TrainedByVersion > 0 {
				ref["version"] = c.TrainedByVersion
			}
			payload["definitionRef"] = ref
		}
		lr := schema.LinkedRecord{
			ID:        id.New(d.Clock, "linked-record"),
			Type:      "linked-definition",
			Version:   1,
			State:     "ACTIVE",
			KeyedTo:   schema.LinkedRecordKeyedTo{Kind: schema.LinkedRecordKeyedToKindDefinition, ID: def.ID},
			Payload:   payload,
			CreatedAt: now,
		}
		if err := insertLinkedRecord(ctx, tx, def.ID, lr); err != nil {
			return err
		}
	}

	if p := doc.Payment; p != nil {
		payload := map[string]any{
			"definitionVersion": def.Version,
			"authoredByPartyId": def.AuthoredByPartyID,
		}
		if len(p.Roles) > 0 {
			payload["roles"] = p.Roles
		}
		if len(p.Tranches) > 0 {
			tranches := make([]map[string]any, 0, len(p.Tranches))
			for _, t := range p.Tranches {
				m := map[string]any{"label": t.Label}
				if t.Share != "" {
					m["share"] = t.Share
				}
				if t.Condition != "" {
					m["condition"] = t.Condition
				}
				tranches = append(tranches, m)
			}
			payload["tranches"] = tranches
		}
		if len(p.Preconditions) > 0 {
			payload["preconditions"] = p.Preconditions
		}
		if len(p.Deductions) > 0 {
			deductions := make([]map[string]any, 0, len(p.Deductions))
			for _, dd := range p.Deductions {
				deductions = append(deductions, map[string]any{"label": dd.Label, "rule": dd.Rule})
			}
			payload["deductions"] = deductions
		}
		if err := schema.Validate(schema.IDPaymentStructure, payload); err != nil {
			return err
		}
		lr := schema.LinkedRecord{
			ID:        id.New(d.Clock, "linked-record"),
			Type:      "payment-structure",
			Version:   1,
			State:     "ACTIVE",
			KeyedTo:   schema.LinkedRecordKeyedTo{Kind: schema.LinkedRecordKeyedToKindDefinition, ID: def.ID},
			Payload:   payload,
			CreatedAt: now,
		}
		if err := insertLinkedRecord(ctx, tx, def.ID, lr); err != nil {
			return err
		}
	}
	return nil
}

type notReadyError struct{ Problems []Problem }

func (e *notReadyError) Error() string { return "the draft is not ready to submit" }

type sectionError struct{ msg string }

func (e *sectionError) Error() string { return e.msg }

// setSection routes a whole-section write to its field, refusing a section
// name the wizard does not have.
func setSection(doc *DraftDoc, name string, raw json.RawMessage) error {
	into := func(v any) error {
		if err := json.Unmarshal(raw, v); err != nil {
			return &sectionError{msg: "the section body does not parse: " + err.Error()}
		}
		return nil
	}
	switch name {
	case "scope":
		doc.Scope = &ScopeSection{}
		return into(doc.Scope)
	case "activity":
		doc.Activity = &ActivitySection{}
		return into(doc.Activity)
	case "parties":
		doc.Parties = &PartiesSection{}
		return into(doc.Parties)
	case "evidence":
		doc.Evidence = &EvidenceSection{}
		return into(doc.Evidence)
	case "validation":
		doc.Validation = &ValidationSection{}
		return into(doc.Validation)
	case "sources":
		doc.Sources = &SourcesSection{}
		return into(doc.Sources)
	case "cascade":
		doc.Cascade = &CascadeSection{}
		return into(doc.Cascade)
	case "extensions":
		doc.Extensions = map[string]ExtensionField{}
		return into(&doc.Extensions)
	case "payment":
		doc.Payment = &PaymentSection{}
		return into(doc.Payment)
	default:
		return &sectionError{msg: "no such section: " + name +
			" (scope, activity, parties, evidence, validation, sources, cascade, extensions, payment)"}
	}
}

// docFromDefinition rebuilds a wizard document from a ratified version, for
// cloning. Best-effort by design: classification strings return to their
// sections; what a definition does not carry stays empty for the author.
func docFromDefinition(d schema.Definition) DraftDoc {
	str := func(k string) string {
		if v, ok := d.Classification[k].(string); ok {
			return v
		}
		return ""
	}
	doc := DraftDoc{
		Scope: &ScopeSection{Sector: str("sector"), Category: str("category")},
		Activity: &ActivitySection{
			Code:        d.Activity.Code,
			Label:       d.Activity.Label,
			OutcomeUnit: d.OutcomeUnit,
			Counting:    d.Counting,
		},
		Parties: &PartiesSection{
			PerformerRole:     str("performerRole"),
			PartyType:         str("performerPartyType"),
			AttesterFunctions: d.AuthorisedAttesterFunctions,
		},
		Evidence: &EvidenceSection{
			Summary:                 d.Faces.Worker.Summary,
			EvidenceInPlainLanguage: d.Faces.Worker.EvidenceInPlainLanguage,
			TierCeiling:             d.Faces.Worker.TierCeiling,
			CheckIntensity:          str("checkIntensity"),
			TierMap:                 d.TierMap,
		},
		Validation: &ValidationSection{
			AuthorisedIssuers: d.Faces.Verifier.AuthorisedIssuers,
			SpecifierPartyID:  d.Faces.Verifier.SpecifierPartyID,
			Posture:           str("validationPosture"),
		},
		Sources: &SourcesSection{
			SourceSystems:  d.Faces.Platform.SourceSystems,
			RequiredFields: d.Faces.Platform.RequiredFields,
			SchemaRef:      d.Faces.Platform.SchemaRef,
		},
	}
	if d.SkillCode != nil {
		doc.Activity.SkillCode = *d.SkillCode
	}
	// validationDelayDays travels as a classification string; a value that
	// does not parse stays unset rather than guessed.
	if v, err := strconv.Atoi(str("validationDelayDays")); err == nil {
		doc.Validation.DelayDays = &v
	}
	if len(d.Extensions) > 0 {
		doc.Extensions = map[string]ExtensionField{}
		for k, v := range d.Extensions {
			if m, ok := v.(map[string]any); ok {
				f := ExtensionField{}
				f.Label, _ = m["label"].(string)
				f.ValueType, _ = m["valueType"].(string)
				f.Value, _ = m["value"].(string)
				doc.Extensions[k] = f
			}
		}
	}
	return doc
}
