package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/theflywheel/crest/adapters"
	csvadapter "github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
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
	// Working the queue, not just listing it (#25). See unclear.go for the
	// three decisions built into re-attribution.
	mux.HandleFunc("POST /v1/unclear/{id}/resolve", hs.resolveUnclear)

	// Source heartbeat monitoring (#22). A source going quiet is the one
	// failure a worker cannot see and cannot report.
	mux.HandleFunc("POST /v1/sources", hs.registerSource)
	mux.HandleFunc("GET /v1/sources", hs.listSources)
	mux.HandleFunc("POST /v1/sources/sweep", hs.sweepSources)
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

	// The source's own vocabulary, from configuration (#25). An unregistered
	// source gets an empty mapping and the file is read as canonical column
	// names — which is every fixture in this repo, and the reason registering a
	// source is a deployment step rather than a precondition for ingesting
	// anything at all.
	mapping, err := mappingFor(r.Context(), h.d.DB.Q(), adapter.Ref(), params.ContextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read the source mapping", err)
		return
	}
	params.Source.Mapping = mapping

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
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	claims, err := listClaims(r.Context(), h.d.DB.Q(), ids, r.URL.Query().Get("state"))
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

// registerSource declares a feed this deployment expects evidence from (#22).
//
// The cadence and the owner are both required and neither is defaulted. A
// cadence inferred from history would learn from a degraded feed and call it
// healthy; an owner defaulted to nobody produces an alert that gets forwarded
// until it is nobody's.
func (h *handlers) registerSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AdapterRef    string           `json:"adapterRef"`
		ContextID     string           `json:"contextId"`
		SystemRef     string           `json:"systemRef"`
		ExpectedEvery string           `json:"expectedEvery"`
		OwnerPartyID  string           `json:"ownerPartyId"`
		Mapping       adapters.Mapping `json:"mapping"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	for name, v := range map[string]string{
		"adapterRef": body.AdapterRef, "contextId": body.ContextID,
		"expectedEvery": body.ExpectedEvery, "ownerPartyId": body.OwnerPartyID,
	} {
		if v == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
				"%s is required; a source with no %s cannot be monitored", name, name)
			return
		}
	}
	every, err := time.ParseDuration(body.ExpectedEvery)
	if err != nil || every <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"expectedEvery must be a positive duration such as \"24h\"")
		return
	}

	src := Source{
		ID:            id.New(h.d.Clock, "source"),
		AdapterRef:    body.AdapterRef,
		ContextID:     body.ContextID,
		SystemRef:     body.SystemRef,
		OwnerPartyID:  body.OwnerPartyID,
		Mapping:       body.Mapping,
		RegisteredAt:  h.d.Clock.Now(),
		expectedEvery: every,
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		src, err = registerSource(r.Context(), tx, src)
		return err
	}); err != nil {
		httpx.Fail(w, h.d.Log, "register source", err)
		return
	}
	src.stateAt(h.d.Clock.Now())
	httpx.WriteJSON(w, http.StatusCreated, src)
}

// listSources is what an operations console reads. `?state=SILENT` is the query
// somebody should be able to alert on.
func (h *handlers) listSources(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	sources, err := listSources(r.Context(), h.d.DB.Q(), now)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list sources", err)
		return
	}
	if want := r.URL.Query().Get("state"); want != "" {
		filtered := make([]Source, 0, len(sources))
		for _, s := range sources {
			if string(s.State) == want {
				filtered = append(filtered, s)
			}
		}
		sources = filtered
	}
	if sources == nil {
		sources = []Source{}
	}
	// The count is here so a monitor can alert on a number without parsing the
	// list, and silent is broken out because it is the only one anybody pages on.
	silent := 0
	for _, s := range sources {
		if s.overdue(now) {
			silent++
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"sources": sources, "count": len(sources), "silent": silent,
	})
}

// sweepSources notices which feeds have gone quiet and tells their owners.
//
// Driven by a call rather than a background ticker, exactly like confirmation's
// window sweep: a test that has to wait for a goroutine is a test that is flaky
// on a slow runner, and an operator who cannot make it run now has no way to
// check their fix worked.
func (h *handlers) sweepSources(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	sources, err := listSources(r.Context(), h.d.DB.Q(), now)
	if err != nil {
		httpx.Fail(w, h.d.Log, "sweep sources", err)
		return
	}

	opened := []string{}
	stillSilent := []string{}
	for _, s := range sources {
		if !s.overdue(now) {
			continue
		}
		var isNew bool
		if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
			var err error
			if isNew, err = openSilence(r.Context(), tx, s.ID, now); err != nil || !isNew {
				return err
			}
			// Enqueued in the transaction that opened the episode, so the
			// alert cannot be lost by a crash between noticing and telling —
			// and cannot be sent twice, because the episode only opens once.
			return store.Enqueue(r.Context(), tx, topicSourceQuiet, map[string]any{
				"partyId": s.OwnerPartyID,
				"kind":    "source-went-quiet",
				// The feed by the name its operator knows it by, not the
				// internal id: the person receiving this has to go and look at
				// something, and a ULID is not something they can look at.
				"subject": s.SystemRef,
			})
		}); err != nil {
			httpx.Fail(w, h.d.Log, "record source silence", err)
			return
		}
		if isNew {
			opened = append(opened, s.ID)
			h.d.Log.Warn("a source has gone quiet",
				"source", s.ID, "adapter", s.AdapterRef, "context", s.ContextID,
				"quietFor", s.QuietFor, "owner", s.OwnerPartyID)
		} else {
			stillSilent = append(stillSilent, s.ID)
		}
	}
	// Both lists are returned. A sweep that reported only what it discovered
	// would read as "nothing wrong" on the second run of an unfixed outage.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"wentQuiet":  opened,
		"stillQuiet": stillSilent,
		"checked":    len(sources),
	})
}
