package definitions

import (
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

func routes(mux *http.ServeMux, d service.Deps) {
	h := &handlers{d: d}

	mux.HandleFunc("POST /v1/definitions", h.create)
	mux.HandleFunc("GET /v1/definitions/{id}", h.get)
	mux.HandleFunc("GET /v1/definitions/{id}/faces/{face}", h.face)
	mux.HandleFunc("GET /v1/definitions/{id}/publication", h.publication)
	mux.HandleFunc("POST /v1/definitions/{id}/versions/{version}/ratify", h.ratify)
	mux.HandleFunc("POST /v1/definitions/{id}/versions/{version}/activate", h.activate)
	mux.HandleFunc("POST /v1/definitions/{id}/linked-records", h.addLinkedRecord)
	mux.HandleFunc("GET /v1/definitions/{id}/linked-records", h.listLinkedRecords)
	mux.HandleFunc("GET /v1/definitions/{id}/events", h.events)
	mux.HandleFunc("GET /v1/definitions/{id}/versions/{version}/template", h.template)
	mux.HandleFunc("POST /v1/definitions/{id}/versions/{version}/payment-handoff", h.paymentHandoff)
	mux.HandleFunc("GET /v1/adaptors", h.adaptors)

	// Authoring (P-3): the mutable draft layer over the immutable table.
	dh := &draftHandlers{d: d}
	mux.HandleFunc("POST /v1/definition-drafts", dh.createDraft)
	mux.HandleFunc("GET /v1/definition-drafts", dh.listDrafts)
	mux.HandleFunc("GET /v1/definition-drafts/{id}", dh.getDraft)
	mux.HandleFunc("PUT /v1/definition-drafts/{id}/sections/{section}", dh.putSection)
	mux.HandleFunc("POST /v1/definition-drafts/{id}/discard", dh.discard)
	mux.HandleFunc("POST /v1/definition-drafts/{id}/validate", dh.validate)
	mux.HandleFunc("POST /v1/definition-drafts/{id}/dry-run", dh.dryRun)
	mux.HandleFunc("POST /v1/definition-drafts/{id}/submit", dh.submit)
}

type handlers struct{ d service.Deps }

func (h *handlers) create(w http.ResponseWriter, r *http.Request) {
	var def schema.Definition
	if !httpx.ReadJSON(w, r, &def) {
		return
	}
	if def.ID == "" {
		def.ID = id.New(h.d.Clock, "definition")
	}
	if def.Version == 0 {
		def.Version = 1
	}
	if def.CreatedAt.IsZero() {
		def.CreatedAt = h.d.Clock.Now()
	}

	// A definition may be created straight into ACTIVE only by a caller that
	// supplies a ratifier, and the constraint below still refuses a self-
	// ratified one. Seeding the fixture world uses that path; a person never
	// should, which is why the two-step route exists at all.
	if def.State == "" {
		def.State = schema.DefinitionStateDRAFT
	}
	if def.State != schema.DefinitionStateDRAFT && def.RatifiedByPartyID == nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "unratified",
			"a definition past DRAFT needs a ratifier; ratify it rather than declaring it approved")
		return
	}
	if def.RatifiedByPartyID != nil && *def.RatifiedByPartyID == def.AuthoredByPartyID {
		httpx.WriteError(w, http.StatusConflict, "self_ratified", "%s", ErrSelfRatified)
		return
	}
	if err := schema.Validate(schema.IDDefinition, def); err != nil {
		writeValidation(w, err)
		return
	}

	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertDefinition(r.Context(), tx, def)
	}); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			httpx.WriteError(w, http.StatusConflict, "already_exists", "%s", ErrAlreadyExists)
			return
		}
		httpx.Fail(w, h.d.Log, "create definition", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, def)
}

func (h *handlers) get(w http.ResponseWriter, r *http.Request) {
	def, err := h.lookup(w, r)
	if err != nil {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, def)
}

// face returns one projection. Three faces, one signed record — never three
// documents (§7) — so this reads the same row three ways rather than storing
// three things that can disagree.
func (h *handlers) face(w http.ResponseWriter, r *http.Request) {
	def, err := h.lookup(w, r)
	if err != nil {
		return
	}
	common := map[string]any{
		"definitionId": def.ID,
		"version":      def.Version,
		"activity":     def.Activity,
		"outcomeUnit":  def.OutcomeUnit,
		"state":        def.State,
	}
	switch r.PathValue("face") {
	case "worker":
		common["worker"] = def.Faces.Worker
	case "platform":
		common["platform"] = def.Faces.Platform
	case "verifier":
		// The verifier face carries the tier map, because a verifier who cannot
		// see the evidence-to-tier map cannot check the tier they are shown —
		// they can only be told it.
		common["verifier"] = def.Faces.Verifier
		common["tierMap"] = def.TierMap
	default:
		httpx.WriteError(w, http.StatusNotFound, "no_such_face",
			"the faces are worker, platform and verifier (§7)")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, common)
}

// publication answers "where can a verifier resolve the version this credential
// pinned?" — the question §7 says a definition has to be able to answer to
// someone who does not trust CREST.
//
// It reports transparent explicitly. A caller that receives a publication with
// transparent=false has been told, in the same response, that resolving it
// still means trusting this deployment.
func (h *handlers) publication(w http.ResponseWriter, r *http.Request) {
	version := 0
	if s := r.URL.Query().Get("version"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "version is not a number")
			return
		}
		version = v
	}
	if version == 0 {
		def, err := h.lookup(w, r)
		if err != nil {
			return
		}
		version = def.Version
	}
	pub, err := publicationOf(r.Context(), h.d.DB.Q(), r.PathValue("id"), version)
	if errors.Is(err, store.ErrNotFound) {
		// Distinguished from "no such definition" on purpose: a version that
		// exists and is not yet published is a normal, temporary state, and a
		// caller that cannot tell it from a typo will retry the wrong thing.
		httpx.WriteError(w, http.StatusNotFound, "not_published",
			"version %d of this definition has not reached the registry yet", version)
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "read publication", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pub)
}

func (h *handlers) ratify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RatifiedByPartyID string `json:"ratifiedByPartyId"`
		// PendingFields is the ratifier's own declaration (p3_15): what they
		// judged acceptable to leave open. RATIFIED-with-pending is a real
		// state, not a blocked one, and only the approver may declare it.
		PendingFields []string `json:"pendingFields,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	version, ok := h.version(w, r)
	if !ok {
		return
	}

	var out schema.Definition
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		out, err = transition(r.Context(), tx, r.PathValue("id"), version,
			schema.DefinitionStateDRAFT, schema.DefinitionStateRATIFIED,
			func(d *schema.Definition) error {
				return applyRatification(d, body.RatifiedByPartyID, body.PendingFields)
			})
		if err != nil {
			return err
		}
		// The two-records-one-signature flow (p3_16): the state change and
		// the governance event commit together or not at all.
		detail := map[string]any{}
		if len(out.PendingFields) > 0 {
			detail["pendingFields"] = out.PendingFields
		}
		return appendEvent(r.Context(), tx, out.ID, out.Version,
			eventRatified, body.RatifiedByPartyID, h.d.Clock.Now(), detail)
	})
	switch {
	case errors.Is(err, ErrSelfRatified):
		httpx.WriteError(w, http.StatusConflict, "self_ratified", "%s", ErrSelfRatified)
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such definition version")
	case err != nil:
		httpx.WriteError(w, http.StatusConflict, "cannot_ratify", "%v", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *handlers) activate(w http.ResponseWriter, r *http.Request) {
	version, ok := h.version(w, r)
	if !ok {
		return
	}
	// The actor is optional in the body for compatibility with pre-authoring
	// callers; absent, the event names the ratifier, because in the wizard's
	// sign-and-publish the same signature does both (p3_16).
	var body struct {
		ActivatedByPartyID string `json:"activatedByPartyId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var out schema.Definition
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		out, err = transition(r.Context(), tx, r.PathValue("id"), version,
			schema.DefinitionStateRATIFIED, schema.DefinitionStateACTIVE,
			func(d *schema.Definition) error {
				at := h.d.Clock.Now()
				d.ActivatedAt = &at
				return nil
			})
		if err != nil {
			return err
		}
		actor := body.ActivatedByPartyID
		if actor == "" && out.RatifiedByPartyID != nil {
			actor = *out.RatifiedByPartyID
		}
		if err := appendEvent(r.Context(), tx, out.ID, out.Version,
			eventActivated, actor, h.d.Clock.Now(), nil); err != nil {
			return err
		}
		// In the same transaction as the state change. A publish attempted
		// after the commit is a publish a crash loses, and the result is an
		// ACTIVE definition that no verifier can resolve — which is exactly
		// the state credentials would then be issued against (§3, §7).
		return enqueuePublication(r.Context(), tx, out.ID, out.Version)
	})
	switch {
	case errors.Is(err, ErrImmutable):
		httpx.WriteError(w, http.StatusConflict, "immutable", "%s", ErrImmutable)
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such definition version")
	case err != nil:
		httpx.WriteError(w, http.StatusConflict, "cannot_activate", "%v", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *handlers) addLinkedRecord(w http.ResponseWriter, r *http.Request) {
	var lr schema.LinkedRecord
	if !httpx.ReadJSON(w, r, &lr) {
		return
	}
	if lr.ID == "" {
		lr.ID = id.New(h.d.Clock, "linked-record")
	}
	if lr.CreatedAt.IsZero() {
		lr.CreatedAt = h.d.Clock.Now()
	}
	if lr.Version == 0 {
		lr.Version = 1
	}
	if lr.State == "" {
		lr.State = "ACTIVE"
	}
	lr.KeyedTo = schema.LinkedRecordKeyedTo{
		Kind: schema.LinkedRecordKeyedToKindDefinition,
		ID:   r.PathValue("id"),
	}
	if err := schema.Validate(schema.IDLinkedRecord, lr); err != nil {
		writeValidation(w, err)
		return
	}
	// The core stores a payload opaquely, but somebody has to check it, and for
	// a record keyed to a definition that somebody is this service.
	switch lr.Type {
	case "payment-setup":
		if err := schema.Validate(schema.IDPaymentSetup, lr.Payload); err != nil {
			writeValidation(w, err)
			return
		}
	case "payment-structure":
		if err := schema.Validate(schema.IDPaymentStructure, lr.Payload); err != nil {
			writeValidation(w, err)
			return
		}
	case "payment-handoff":
		if err := schema.Validate(schema.IDPaymentHandoff, lr.Payload); err != nil {
			writeValidation(w, err)
			return
		}
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertLinkedRecord(r.Context(), tx, r.PathValue("id"), lr)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "attach linked record", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, lr)
}

func (h *handlers) listLinkedRecords(w http.ResponseWriter, r *http.Request) {
	records, err := linkedRecords(r.Context(), h.d.DB.Q(), r.PathValue("id"), r.URL.Query().Get("type"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list linked records", err)
		return
	}
	if records == nil {
		records = []schema.LinkedRecord{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"linkedRecords": records})
}

// events serves the append-only governance record: who did what to which
// version, when. This is where a dispute about a ratification goes.
func (h *handlers) events(w http.ResponseWriter, r *http.Request) {
	evs, err := listEvents(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list definition events", err)
		return
	}
	if evs == nil {
		evs = []Event{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"events": evs})
}

// paymentHandoff records the hand-over of pricing to a rate owner (p3_pay):
// the definition is signed, nothing prices it yet, and that is a complete
// state made visible rather than an error. The record composes with the
// payments service's RateOwnerAssignment (F-1) — the invitation lives here,
// the authority lives there, and neither substitutes for the other.
func (h *handlers) paymentHandoff(w http.ResponseWriter, r *http.Request) {
	version, ok := h.version(w, r)
	if !ok {
		return
	}
	var body struct {
		InvitedPartyID   string `json:"invitedPartyId,omitempty"`
		InvitedByPartyID string `json:"invitedByPartyId"`
		Note             string `json:"note,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}

	def, err := getDefinition(r.Context(), h.d.DB.Q(), r.PathValue("id"), version)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "definition version", err, store.ErrNotFound)
		return
	}
	now := h.d.Clock.Now()
	payload := map[string]any{
		"definitionVersion": def.Version,
		"invitedByPartyId":  body.InvitedByPartyID,
		"invitedAt":         now.UTC().Format(time.RFC3339),
	}
	if body.InvitedPartyID != "" {
		payload["invitedPartyId"] = body.InvitedPartyID
	}
	if body.Note != "" {
		payload["note"] = body.Note
	}
	if err := schema.Validate(schema.IDPaymentHandoff, payload); err != nil {
		writeValidation(w, err)
		return
	}
	lr := schema.LinkedRecord{
		ID:        id.New(h.d.Clock, "linked-record"),
		Type:      "payment-handoff",
		Version:   1,
		State:     "ACTIVE",
		KeyedTo:   schema.LinkedRecordKeyedTo{Kind: schema.LinkedRecordKeyedToKindDefinition, ID: def.ID},
		Payload:   payload,
		CreatedAt: now,
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertLinkedRecord(r.Context(), tx, def.ID, lr); err != nil {
			return err
		}
		detail := map[string]any{}
		if body.InvitedPartyID != "" {
			detail["invitedPartyId"] = body.InvitedPartyID
		}
		return appendEvent(r.Context(), tx, def.ID, def.Version,
			eventHandoff, body.InvitedByPartyID, now, detail)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record payment handoff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, lr)
}

func (h *handlers) lookup(w http.ResponseWriter, r *http.Request) (schema.Definition, error) {
	version := 0
	if s := r.URL.Query().Get("version"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "version is not a number")
			return schema.Definition{}, err
		}
		version = v
	}
	def, err := getDefinition(r.Context(), h.d.DB.Q(), r.PathValue("id"), version)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "definition", err, store.ErrNotFound)
		return schema.Definition{}, err
	}
	return def, nil
}

func (h *handlers) version(w http.ResponseWriter, r *http.Request) (int, bool) {
	v, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "version is not a number")
		return 0, false
	}
	return v, true
}

func writeValidation(w http.ResponseWriter, err error) {
	var ve *schema.ValidationError
	if errors.As(err, &ve) {
		httpx.WriteProblems(w, "schema_violation", "the document does not satisfy "+ve.SchemaID, ve.Problems)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "validation failed: %v", err)
}
