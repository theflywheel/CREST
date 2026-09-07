package evidence

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theflywheel/crest/adapters"
	csvadapter "github.com/theflywheel/crest/adapters/csv"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
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

	adapterRegistry, err := adapters.NewRegistry(csvadapter.Plugin())
	if err != nil {
		d.Log.Error("adapter catalogue is invalid", "error", err)
		panic(err)
	}
	hs := &handlers{
		d:        d,
		adapters: adapterRegistry,
		in: &ingestor{
			registry:    client.New(config.Str("PARTIES_URL", "http://parties:8080")),
			definitions: client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
			clock:       d.Clock,
		},
	}

	mux.HandleFunc("POST /v1/batches", hs.submitBatch)
	mux.HandleFunc("GET /v1/batches/{id}", hs.getBatch)
	// w6_3: the project-side receipt for what a batch brought in. See receipt.go.
	mux.HandleFunc("GET /v1/batches/{id}/receipt", hs.getBatchReceipt)
	// g4_7: registry reuse. See reuse.go.
	mux.HandleFunc("GET /v1/registry-reuse", hs.registryReuse)
	// Record reads by id stay open (#102): the ids are unguessable ULIDs and
	// carrying one is how a printed artefact or a support escalation names a
	// record — capability semantics, the same judgement as a credential id.
	// The LIST is the enumerable surface, and that one is authorized.
	mux.HandleFunc("GET /v1/units/{id}", hs.getUnit)
	mux.HandleFunc("GET /v1/claims", hs.listClaims)
	mux.HandleFunc("GET /v1/claims/{id}", hs.getClaim)
	// The claim state machine is confirmation's to drive, and nobody else's:
	// internal only (#102, service-identity ruling).
	mux.HandleFunc("POST /internal/claims/{id}/transition", hs.transition)
	mux.HandleFunc("GET /internal/units/{id}", hs.getInternalUnit)
	mux.HandleFunc("GET /internal/claims/{id}", hs.getInternalClaim)
	mux.HandleFunc("GET /v1/unclear", hs.listUnclear)
	// Working the queue, not just listing it (#25). See unclear.go for the
	// three decisions built into re-attribution.
	mux.HandleFunc("POST /v1/unclear/{id}/resolve", hs.resolveUnclear)

	// Source heartbeat monitoring (#22). A source going quiet is the one
	// failure a worker cannot see and cannot report.
	mux.HandleFunc("POST /v1/sources", hs.registerSource)
	mux.HandleFunc("GET /v1/sources", hs.listSources)
	mux.HandleFunc("POST /v1/sources/sweep", hs.sweepSources)
	mux.HandleFunc("GET /v1/adapters", hs.listAdapters)
	// Service-boundary lookup used when verification records a source
	// assessment. The caller cannot supply provenance or ownership here; this
	// route returns the registration approved by the evidence service.
	mux.HandleFunc("GET /internal/sources/by-system/{systemRef}", hs.getInternalSource)
}

type handlers struct {
	d        service.Deps
	in       *ingestor
	adapters adapters.Registry
}

const functionDefinitionSourceOwner = "work-definition-source-owner"

// submitBatch takes the file itself as the body and everything the deployment
// knows about the source as query parameters.
//
// The split is the point: the payload is what the source system said, and the
// parameters are what the deployment knows about that source. Provenance comes
// from the second, never the first (§8).
func (h *handlers) submitBatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	version, err := strconv.Atoi(q.Get("definitionVersion"))
	if err != nil || version <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_parameter",
			"definitionVersion is required and must be a positive integer")
		return
	}
	params := ingestParams{
		ContextID:         q.Get("contextId"),
		DefinitionID:      q.Get("definitionId"),
		DefinitionVersion: version,
		SubmittedBy:       q.Get("submittedBy"),
		Source:            adapters.Source{SystemRef: q.Get("systemRef")},
	}
	for name, value := range map[string]string{
		"contextId": params.ContextID, "definitionId": params.DefinitionID,
		"submittedBy": params.SubmittedBy, "systemRef": params.Source.SystemRef,
	} {
		if value == "" {
			httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
				"%s is required: a batch with unknown provenance cannot be assessed for strength", name)
			return
		}
	}

	// The submitter is the caller (#102): submittedBy is what permits() is
	// checked against and what every claim records as its provenance, and
	// until now it was a query parameter anybody could type.
	if _, ok := identity.Authorize(w, r, h.d.Log, params.SubmittedBy, params.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, (64<<20)+1))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "unreadable_body", "could not read the batch: %v", err)
		return
	}

	if len(body) > 64<<20 {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "batch_too_large", "batch exceeds 64 MiB; no rows were accepted")
		return
	}
	// The source's own vocabulary and adapter version both come from approved
	// registration. The request can name a source instance, but it cannot pick
	// a parser or assert its own provenance.
	registered, err := registeredSource(r.Context(), h.d.DB.Q(), params.ContextID, params.Source.SystemRef)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusForbidden, "source_not_registered",
			"the source %q is not registered for this project; provenance must be approved before ingestion", params.Source.SystemRef)
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "read the registered source", err)
		return
	}
	if requested := strings.TrimSpace(q.Get("adapterRef")); requested != "" && requested != registered.AdapterRef {
		httpx.WriteError(w, http.StatusForbidden, "source_adapter_mismatch",
			"adapterRef must match the registered source; parser selection is deployment configuration")
		return
	}
	adapter, ok := h.adapters.Lookup(registered.AdapterRef)
	if !ok {
		httpx.WriteError(w, http.StatusServiceUnavailable, "adapter_unavailable",
			"the registered adapter version is not available in this deployment")
		return
	}
	params.Source.AdapterRef = registered.AdapterRef
	params.Source.Class, params.Source.CaptureMethod, params.Source.Exposure = registered.Class, registered.CaptureMethod, registered.Exposure
	params.Source.Mapping = registered.Mapping
	if !params.Source.ValidProvenance() || registered.AdapterRef != adapter.Ref() {
		httpx.WriteError(w, http.StatusForbidden, "source_not_approved",
			"the registered source has incomplete or unsupported provenance")
		return
	}

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
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	b, err := getBatch(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "batch", err, store.ErrNotFound)
		return
	}
	if !h.authorizeContext(w, r, b.ContextID) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, b)
}

func (h *handlers) getUnit(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	u, err := getUnit(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "unit", err, store.ErrNotFound)
		return
	}
	if !h.authorizeUnit(w, r, u) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

// Internal consumers (credential issuance and verification) are authenticated
// by the service boundary middleware. They do not carry a worker caller, so
// the public party/project scope check must not be applied to this route.
func (h *handlers) getInternalUnit(w http.ResponseWriter, r *http.Request) {
	u, err := getStoredUnit(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "unit", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

func (h *handlers) getClaim(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	c, err := getClaim(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "claim", err, store.ErrNotFound)
		return
	}
	u, err := getUnit(r.Context(), h.d.DB.Q(), c.UnitID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read claim unit", err)
		return
	}
	if !h.authorizeClaim(w, r, c, u) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

// getInternalClaim is the service-boundary view used by credential issuance.
// It returns the authoritative claim document without accepting a party or
// state assertion from the caller.
func (h *handlers) getInternalClaim(w http.ResponseWriter, r *http.Request) {
	c, err := getClaim(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "claim", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (h *handlers) listClaims(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	// A party-filtered list is that worker's history; an unfiltered list is
	// everybody's. The first answers the worker or their actor, the second a
	// signed-in operator (#102). The merge expansion runs BEFORE the identity
	// check and the check is against the survivor: a worker whose duplicate
	// was closed authenticates as who they are now, and their stale bookmark
	// naming the absorbed id must still be their own history, not an
	// impersonation refusal (#100).
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	if r.URL.Query().Get("partyId") != "" {
		if _, ok := identity.Authorize(w, r, h.d.Log, ids[0], "",
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	} else {
		if !h.authorizeContext(w, r, r.URL.Query().Get("contextId")) {
			return
		}
	}
	claims, err := listClaims(r.Context(), h.d.DB.Q(), ids, r.URL.Query().Get("state"), r.URL.Query().Get("contextId"))
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
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	// A queue of unattributed work — names and rows. Signed-in callers (#102).
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	if !h.authorizeContext(w, r, contextID) {
		return
	}
	rows, err := openUnclear(r.Context(), h.d.DB.Q(), contextID)
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
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	var body struct {
		AdapterRef     string                `json:"adapterRef"`
		ContextID      string                `json:"contextId"`
		SystemRef      string                `json:"systemRef"`
		SourceClass    schema.SourceClass    `json:"sourceClass"`
		CaptureMethod  schema.CaptureMethod  `json:"captureMethod"`
		SourceExposure schema.SourceExposure `json:"sourceExposure"`
		ExpectedEvery  string                `json:"expectedEvery"`
		OwnerPartyID   string                `json:"ownerPartyId"`
		Mapping        adapters.Mapping      `json:"mapping"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	for name, v := range map[string]string{
		"adapterRef": body.AdapterRef, "contextId": body.ContextID,
		"systemRef": body.SystemRef, "expectedEvery": body.ExpectedEvery, "ownerPartyId": body.OwnerPartyID,
	} {
		if v == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
				"%s is required; a source with no %s cannot be monitored", name, name)
			return
		}
	}
	if body.SourceClass == "" || body.CaptureMethod == "" || body.SourceExposure == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"sourceClass, captureMethod, and sourceExposure are required approved provenance")
		return
	}
	if !(adapters.Source{AdapterRef: body.AdapterRef, Class: body.SourceClass, CaptureMethod: body.CaptureMethod, Exposure: body.SourceExposure, SystemRef: body.SystemRef}).ValidProvenance() {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "source provenance contains an unsupported value")
		return
	}
	if _, ok := h.adapters.Lookup(body.AdapterRef); !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "adapterRef %q is not a registered adapter version", body.AdapterRef)
		return
	}
	if !h.authorizeSourceOwner(w, r, body.OwnerPartyID, body.ContextID) {
		return
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
		Class:         body.SourceClass,
		CaptureMethod: body.CaptureMethod,
		Exposure:      body.SourceExposure,
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

// authorizeSourceOwner requires both identity and the project-scoped
// governance function. A source registration controls provenance for every
// later batch, so a caller merely naming their own Party must not be able to
// self-elevate a feed to national-system or other trusted provenance.
func (h *handlers) authorizeSourceOwner(w http.ResponseWriter, r *http.Request,
	claimedOwner, contextID string) bool {
	if h.d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the source-owner authorization registry is unavailable")
		return false
	}
	actor, ok := identity.Authorize(w, r, h.d.Log, claimedOwner, contextID, true, h.d.Permits)
	if !ok {
		return false
	}
	permitted, err := h.d.Permits(r.Context(), actor, functionDefinitionSourceOwner, contextID)
	if err != nil {
		h.d.Log.Error("could not check source-owner authorization", "function", functionDefinitionSourceOwner,
			"context", contextID, "actor", actor, "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the source-owner authorization registry could not answer this request")
		return false
	}
	if !permitted {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"the authenticated owner has no source-owner authorization in this project")
		return false
	}
	return true
}

// listAdapters exposes the statically linked parser versions available to the
// deployment. It is private because the catalogue is an operational/configuration
// surface, while its entries contain no source records or credential data.
func (h *handlers) listAdapters(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"adapters": h.adapters.Catalogue()})
}

// listSources is what an operations console reads. `?state=SILENT` is the query
// somebody should be able to alert on.
func (h *handlers) listSources(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	if !h.authorizeContext(w, r, contextID) {
		return
	}
	now := h.d.Clock.Now()
	sources, err := listSources(r.Context(), h.d.DB.Q(), now, contextID)
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

func (h *handlers) getInternalSource(w http.ResponseWriter, r *http.Request) {
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	if contextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter", "contextId is required")
		return
	}
	sources, err := listSources(r.Context(), h.d.DB.Q(), h.d.Clock.Now(), contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read registered source", err)
		return
	}
	for _, source := range sources {
		if source.SystemRef == r.PathValue("systemRef") {
			httpx.WriteJSON(w, http.StatusOK, source)
			return
		}
	}
	httpx.WriteError(w, http.StatusNotFound, "source_not_registered", "no registered source has that system reference")
}

// sweepSources notices which feeds have gone quiet and tells their owners.
//
// Driven by a call rather than a background ticker, exactly like confirmation's
// window sweep: a test that has to wait for a goroutine is a test that is flaky
// on a slow runner, and an operator who cannot make it run now has no way to
// check their fix worked.
func (h *handlers) sweepSources(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	if !h.authorizeContext(w, r, contextID) {
		return
	}
	now := h.d.Clock.Now()
	sources, err := listSources(r.Context(), h.d.DB.Q(), now, contextID)
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
				"partyId":  s.OwnerPartyID,
				"sourceId": s.ID,
				"kind":     "source-went-quiet",
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

// Private evidence surfaces always require a verified caller, including local
// development where the deployment has no configured identity provider. This
// prevents a permissive local branch from accidentally becoming a production
// privacy bypass.
func requirePrivateCaller(w http.ResponseWriter, r *http.Request, d service.Deps) bool {
	if identity.From(r.Context()).Authenticated() {
		return true
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpx.WriteError(w, http.StatusUnauthorized, "no_caller", "this private evidence surface requires a verified caller")
	return false
}

func (h *handlers) authorizeContext(w http.ResponseWriter, r *http.Request, contextID string) bool {
	if strings.TrimSpace(contextID) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter", "contextId is required to scope private evidence")
		return false
	}
	caller := identity.From(r.Context())
	if caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "subject_not_enrolled", "the caller is not enrolled in this deployment")
		return false
	}
	if h.d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorisation_unavailable", "the registry authorisation check is unavailable")
		return false
	}
	ok, err := h.d.Permits(r.Context(), caller.PartyID, "read-work-evidence", contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check evidence read authorization", err)
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "not_authorised", "the caller may not read evidence in %s", contextID)
		return false
	}
	return true
}

func (h *handlers) authorizeUnit(w http.ResponseWriter, r *http.Request, u schema.Unit) bool {
	caller := identity.From(r.Context())
	if caller.PartyID != "" {
		claims, err := unitClaims(r.Context(), h.d.DB.Q(), u.ID)
		if err == nil {
			for _, c := range claims {
				if c.PartyID == caller.PartyID {
					return true
				}
			}
		}
	}
	return h.authorizeContext(w, r, u.ContextID)
}

func (h *handlers) authorizeClaim(w http.ResponseWriter, r *http.Request, c schema.Claim, u schema.Unit) bool {
	if identity.From(r.Context()).PartyID == c.PartyID {
		return true
	}
	return h.authorizeContext(w, r, u.ContextID)
}
