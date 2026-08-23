package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/theflywheel/crest/adapters"
	csvadapter "github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/pii"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// hasher is process-wide because the salt is deployment configuration. It is
// built at route registration so a missing salt stops the service starting
// rather than failing on the first batch that carries a national identifier.
var hasher *pii.Hasher

func routes(mux *http.ServeMux, d service.Deps) {
	h, err := pii.NewHasher(
		config.Str("NATIONAL_ID_SALT", "local-development-salt-not-for-any-real-deployment"),
		config.Str("NATIONAL_ID_SALT_REF", "local-1"))
	if err != nil {
		d.Log.Error("national identifier salt is unusable", "error", err)
		panic(err)
	}
	hasher = h

	hs := &handlers{
		d: d,
		in: &ingestor{
			registry:    client.New(config.Str("REGISTRY_URL", "http://registry:8080")),
			definitions: client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
			clock:       d.Clock,
		},
	}

	mux.HandleFunc("POST /v1/batches", hs.submitBatch)
	mux.HandleFunc("GET /v1/batches/{id}", hs.getBatch)
	mux.HandleFunc("GET /v1/units/{id}", hs.getUnit)
	mux.HandleFunc("GET /v1/claims", hs.listClaims)
	mux.HandleFunc("GET /v1/claims/{id}", hs.getClaim)
	mux.HandleFunc("POST /v1/claims/{id}/transition", hs.transition)
	mux.HandleFunc("GET /v1/unclear", hs.listUnclear)
}

type handlers struct {
	d  service.Deps
	in *ingestor
}

// submitBatch takes the file itself as the body and everything the deployment
// knows about the source as query parameters.
//
// The split is the point: the payload is what the source system said, and the
// parameters are what the deployment knows about that source. Provenance comes
// from the second, never the first (§8).
func (h *handlers) submitBatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := ingestParams{
		ContextID:    q.Get("contextId"),
		DefinitionID: q.Get("definitionId"),
		SubmittedBy:  q.Get("submittedBy"),
		Source: adapters.Source{
			Class:         schema.SourceClass(q.Get("sourceClass")),
			CaptureMethod: schema.CaptureMethod(q.Get("captureMethod")),
			Exposure:      schema.SourceExposure(q.Get("sourceExposure")),
			SystemRef:     q.Get("systemRef"),
		},
	}
	for name, value := range map[string]string{
		"contextId": params.ContextID, "definitionId": params.DefinitionID,
		"submittedBy": params.SubmittedBy, "sourceClass": string(params.Source.Class),
		"captureMethod": string(params.Source.CaptureMethod), "sourceExposure": string(params.Source.Exposure),
	} {
		if value == "" {
			httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
				"%s is required: a batch with unknown provenance cannot be assessed for strength", name)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "unreadable_body", "could not read the batch: %v", err)
		return
	}

	adapter := csvadapter.Adapter{}
	rows, rejections, err := adapter.Parse(bytes.NewReader(body), params.Source, h.d.Clock.Now())
	if err != nil {
		// A file whose header is unusable is refused whole, and named. There is
		// nothing to salvage and the sender needs to know which column is missing.
		httpx.WriteError(w, http.StatusUnprocessableEntity, "unparseable_batch", "%v", err)
		return
	}

	result, err := h.in.run(r.Context(), h.d.DB, params, rows, rejections)
	if err != nil {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "batch_refused", "%v", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}

func (h *handlers) getBatch(w http.ResponseWriter, r *http.Request) {
	b, err := getBatch(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "batch", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, b)
}

func (h *handlers) getUnit(w http.ResponseWriter, r *http.Request) {
	u, err := getUnit(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "unit", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *handlers) getClaim(w http.ResponseWriter, r *http.Request) {
	c, err := getClaim(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "claim", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *handlers) listClaims(w http.ResponseWriter, r *http.Request) {
	claims, err := listClaims(r.Context(), h.d.DB.Q(),
		r.URL.Query().Get("partyId"), r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list claims", err)
		return
	}
	if claims == nil {
		claims = []schema.Claim{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"claims": claims})
}

// transition is how confirmation drives the state machine. Every legal move is
// a request, which is what lets the harness make any of them without touching
// the database.
func (h *handlers) transition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To    schema.ClaimState              `json:"to"`
		Route *schema.ClaimConfirmationRoute `json:"route,omitempty"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}

	var out schema.Claim
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		out, err = transitionClaim(r.Context(), tx, r.PathValue("id"), body.To, func(c *schema.Claim) {
			if body.Route == nil {
				return
			}
			at := h.d.Clock.Now()
			if c.Confirmation == nil {
				c.Confirmation = &schema.ClaimConfirmation{WindowOpenedAt: c.CreatedAt, WindowClosesAt: at}
			}
			c.Confirmation.Route = body.Route
			c.Confirmation.At = &at
		})
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such claim")
	case errors.Is(err, ErrIllegalTransition):
		httpx.WriteError(w, http.StatusConflict, "illegal_transition", "%v", err)
	case err != nil:
		httpx.Fail(w, h.d.Log, "transition claim", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *handlers) listUnclear(w http.ResponseWriter, r *http.Request) {
	rows, err := openUnclear(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list unclear rows", err)
		return
	}
	if rows == nil {
		rows = []UnclearRow{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"unclear": rows, "count": len(rows)})
}

func hashNationalID(raw string) string { return hasher.Hash(raw) }

func urlSafe(s string) string { return url.QueryEscape(s) }
