package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The four routes out of a window, named once.
const (
	routeSelf     = "self"
	routeAuto     = "auto"
	routeAssisted = "assisted"
	routeDispute  = "dispute"
)

func routes(mux *http.ServeMux, d service.Deps) {
	window, err := config.Duration("CONFIRMATION_WINDOW", 7*24*time.Hour)
	if err != nil {
		d.Log.Error("confirmation window is unreadable", "error", err)
		panic(err)
	}

	seed, err := credential.SeedFromBase64(config.Str("ISSUER_SEED",
		// A fixed development seed, so a local stack is reproducible. It is not
		// a secret and is not pretending to be: a deployment sets ISSUER_SEED,
		// and key custody is G1 #7.
		"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="))
	if err != nil {
		d.Log.Error("issuer seed unusable", "error", err)
		panic(err)
	}
	issuer, err := credential.NewIssuer(config.Str("ISSUER_ID", "did:crest:issuer:local"), seed)
	if err != nil {
		d.Log.Error("issuer unusable", "error", err)
		panic(err)
	}
	d.Log.Info("issuer ready", "issuer", issuer.ID(), "key", issuer.PublicKeyMultibase())

	h := &handlers{
		d:      d,
		window: window,
		issuer: issuer,
		ex: &exiter{
			db:            d.DB,
			evidence:      client.New(config.Str("EVIDENCE_URL", "http://evidence:8080")),
			definitions:   client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
			issuer:        issuer,
			log:           d.Log,
			statusListURL: config.Str("STATUS_LIST_URL", "http://confirmation:8080/v1/status-list"),
			clock:         d.Clock,
		},
	}

	mux.HandleFunc("POST /v1/windows", h.openWindow)
	mux.HandleFunc("GET /v1/windows/{claimId}", h.getWindow)
	mux.HandleFunc("POST /v1/claims/{claimId}/confirm", h.confirm)
	mux.HandleFunc("POST /v1/claims/{claimId}/dispute", h.dispute)
	mux.HandleFunc("POST /v1/sweep", h.sweep)
	mux.HandleFunc("GET /v1/credentials/{id}", h.getCredential)
	mux.HandleFunc("POST /v1/credentials/{id}/revoke", h.revoke)
	mux.HandleFunc("GET /v1/status-list", h.statusList)
	mux.HandleFunc("GET /v1/issuer", h.issuerInfo)
	mux.HandleFunc("GET /v1/unreleased", h.unreleased)
	mux.HandleFunc("GET /v1/unreached", h.unreached)
	mux.HandleFunc("POST /v1/claims/{claimId}/assist", h.assist)
}

type handlers struct {
	d      service.Deps
	window time.Duration
	issuer *credential.Issuer
	ex     *exiter
}

// openWindow is called by evidence's outbox when a claim is created.
//
// Opening the window and queueing the notification happen together: a window
// nobody was told about is a worker who cannot confirm and cannot dispute,
// which is W2 broken quietly.
func (h *handlers) openWindow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClaimID      string    `json:"claimId"`
		UnitID       string    `json:"unitId"`
		PartyID      string    `json:"partyId"`
		ContextID    string    `json:"contextId"`
		DefinitionID string    `json:"definitionId"`
		Version      int       `json:"definitionVersion"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	now := h.d.Clock.Now()
	win := Window{
		ClaimID: req.ClaimID, UnitID: req.UnitID, PartyID: req.PartyID,
		ContextID: req.ContextID, DefinitionID: req.DefinitionID,
		DefinitionVersion: req.Version,
		OpenedAt:          now,
		ClosesAt:          now.Add(h.window),
	}

	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		created, err := insertWindow(r.Context(), tx, win)
		if err != nil || !created {
			return err // a redelivery: the window already exists, and one is enough
		}
		if err := store.Enqueue(r.Context(), tx, topicNotifyClaim, map[string]any{
			"partyId":  req.PartyID,
			"claimId":  req.ClaimID,
			"kind":     "confirm-your-work",
			"closesAt": win.ClosesAt,
		}); err != nil {
			return err
		}
		return markNotified(r.Context(), tx, req.ClaimID, now)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "open window", err)
		return
	}
	out, err := getWindow(r.Context(), h.d.DB.Q(), req.ClaimID, false)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read window", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (h *handlers) getWindow(w http.ResponseWriter, r *http.Request) {
	win, err := getWindow(r.Context(), h.d.DB.Q(), r.PathValue("claimId"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "window", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, win)
}

// confirm is the worker saying yes, or a supervisor saying it for them.
func (h *handlers) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Route string `json:"route"`
	}
	if r.ContentLength > 0 && !httpx.ReadJSON(w, r, &body) {
		return
	}
	route := body.Route
	if route == "" {
		route = routeSelf
	}
	if route != routeSelf && route != routeAssisted {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_route",
			"confirming is either self or assisted; auto is what the sweep does and dispute is its own endpoint")
		return
	}
	h.finish(w, r, route)
}

// dispute contests the record. It does not contest the money: the release below
// is the same release every other exit makes (W4).
func (h *handlers) dispute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason          string `json:"reason"`
		RaisedByPartyID string `json:"raisedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.Reason == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a dispute needs a reason, or nobody can act on it")
		return
	}

	claimID := r.PathValue("claimId")
	contest := schema.Contest{
		ID:              id.New(h.d.Clock, "contest"),
		Target:          schema.ContestTarget{Kind: schema.ContestTargetKindClaim, ID: claimID},
		RaisedByPartyID: body.RaisedByPartyID,
		RaisedAt:        h.d.Clock.Now(),
		Reason:          body.Reason,
		State:           schema.ContestStateOPEN,
	}
	if err := schema.Validate(schema.IDContest, contest); err != nil {
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			httpx.WriteProblems(w, "schema_violation", "the dispute is not a valid Contest", ve.Problems)
			return
		}
		httpx.Fail(w, h.d.Log, "validate contest", err)
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertContest(r.Context(), tx, contest)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record dispute", err)
		return
	}
	h.finish(w, r, routeDispute)
}

// sweep auto-confirms every window whose time has run out.
//
// Driven by the clock and nothing else, and exposed as an endpoint so the
// harness can advance seven days and then ask for the consequences, rather than
// waiting for a ticker it cannot see.
func (h *handlers) sweep(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	due, err := dueWindows(r.Context(), h.d.DB.Q(), now, 500)
	if err != nil {
		httpx.Fail(w, h.d.Log, "find due windows", err)
		return
	}
	swept := make([]string, 0, len(due))
	for _, win := range due {
		if _, err := h.ex.exit(r.Context(), win.ClaimID, routeAuto); err != nil {
			// One claim failing must not stop the rest: the others are also
			// owed a payment, and a sweep that aborts halfway leaves the
			// remainder silently unpaid.
			h.d.Log.Error("auto-confirm failed", "claim", win.ClaimID, "error", err)
			continue
		}
		swept = append(swept, win.ClaimID)
	}
	// Windows whose worker was never reached are due and deliberately not
	// swept. They are reported here so a sweep can never look like it did
	// nothing when in fact it declined to act on someone's behalf.
	unreached, err := unreachedWindows(r.Context(), h.d.DB.Q(), now)
	if err != nil {
		httpx.Fail(w, h.d.Log, "find unreached windows", err)
		return
	}
	waiting := make([]string, 0, len(unreached))
	for _, win := range unreached {
		waiting = append(waiting, win.ClaimID)
	}
	if len(waiting) > 0 {
		h.d.Log.Warn("windows past T=7 whose worker was never reached; not auto-confirming",
			"count", len(waiting))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"at": now, "due": len(due), "autoConfirmed": swept,
		"heldForSomeoneToLookAt": waiting,
	})
}

// assist is the supervisor-assisted route: a person confirming on behalf of a
// worker who could not be reached (§9).
//
// It is the answer to an unreached window, and it is a person's decision rather
// than a timer's. That is the whole difference: auto-confirmation on a worker
// who never heard is silence the system manufactured, and this is somebody
// taking responsibility for saying the record is true.
func (h *handlers) assist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AssistedByPartyID string `json:"assistedByPartyId"`
	}
	if r.ContentLength > 0 && !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.AssistedByPartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"an assisted confirmation needs the party making it; otherwise nobody is responsible for it")
		return
	}
	claimID := r.PathValue("claimId")
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return markEscalated(r.Context(), tx, claimID, h.d.Clock.Now())
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record escalation", err)
		return
	}
	h.finish(w, r, routeAssisted)
}

// unreached is the counterpart to /v1/unreleased: windows past T=7 whose worker
// was never told, waiting for a person. Both exist so that a promise is a query
// rather than a hope.
func (h *handlers) unreached(w http.ResponseWriter, r *http.Request) {
	rows, err := unreachedWindows(r.Context(), h.d.DB.Q(), h.d.Clock.Now())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list unreached", err)
		return
	}
	if rows == nil {
		rows = []Window{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"windows": rows, "count": len(rows)})
}

func (h *handlers) finish(w http.ResponseWriter, r *http.Request, route string) {
	result, err := h.ex.exit(r.Context(), r.PathValue("claimId"), route)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no window for that claim")
	case err != nil:
		httpx.Fail(w, h.d.Log, "close window", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func (h *handlers) getCredential(w http.ResponseWriter, r *http.Request) {
	c, err := getCredential(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

// revoke flips one bit. Withdrawal is the single central fact about credentials
// (§9), and this is the whole of it.
func (h *handlers) revoke(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		idx, err := revokeCredential(r.Context(), tx, r.PathValue("id"), now)
		if err != nil {
			return err
		}
		list, err := loadStatusList(r.Context(), tx)
		if err != nil {
			return err
		}
		if err := list.Revoke(idx); err != nil {
			return err
		}
		return saveStatusList(r.Context(), tx, list)
	})
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// statusList returns the whole bitstring, signed. Whole, because a verifier who
// asks about one credential tells the issuer which credential they are checking.
func (h *handlers) statusList(w http.ResponseWriter, r *http.Request) {
	list, err := loadStatusList(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "load status list", err)
		return
	}
	doc, err := h.issuer.StatusListCredential(
		config.Str("STATUS_LIST_URL", "http://confirmation:8080/v1/status-list"), list, h.d.Clock.Now())
	if err != nil {
		httpx.Fail(w, h.d.Log, "sign status list", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, doc)
}

// issuerInfo publishes the verification key. A verifier needs it once and then
// never again — which is what makes offline verification possible.
func (h *handlers) issuerInfo(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"issuer":             h.issuer.ID(),
		"verificationMethod": h.issuer.VerificationMethod(),
		"publicKeyMultibase": h.issuer.PublicKeyMultibase(),
		"cryptosuite":        credential.CryptosuiteName,
	})
}

// unreleased should always answer zero. It exists so W4 can be checked rather
// than believed.
func (h *handlers) unreleased(w http.ResponseWriter, r *http.Request) {
	rows, err := unreleased(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list unreleased", err)
		return
	}
	if rows == nil {
		rows = []Window{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"windows": rows, "count": len(rows)})
}
