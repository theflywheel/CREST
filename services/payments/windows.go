package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
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

func windowRoutes(mux *http.ServeMux, d service.Deps) {
	window, err := config.Duration("CONFIRMATION_WINDOW", 7*24*time.Hour)
	if err != nil {
		d.Log.Error("confirmation window is unreadable", "error", err)
		panic(err)
	}

	h := &windowHandlers{
		d:      d,
		window: window,
		ex: &exiter{
			db:       d.DB,
			evidence: client.New(config.Str("EVIDENCE_URL", "http://evidence:8080")),
			// Issuance is requested from the credential substrate, never
			// performed here (#137): this service holds no keys, no status
			// list and no credential record.
			verification: client.New(config.Str("VERIFICATION_URL", "http://verification:8080")),
			log:          d.Log,
			clock:        d.Clock,
		},
	}

	mux.HandleFunc("POST /v1/windows", h.openWindow)
	mux.HandleFunc("GET /v1/windows/{claimId}", h.getWindow)
	// A worker's confirmation history, following any merge (#100).
	mux.HandleFunc("GET /v1/windows", h.listWindows)
	mux.HandleFunc("POST /v1/claims/{claimId}/confirm", h.confirm)
	mux.HandleFunc("POST /v1/claims/{claimId}/dispute", h.dispute)
	mux.HandleFunc("GET /v1/contests", h.contests)
	mux.HandleFunc("POST /v1/sweep", h.sweep)
	// ...and the same sweep on a timer, because on a running deployment nobody
	// posts to that endpoint and a window that goes past T=7 would otherwise
	// stay open, unpaid, forever.
	every, err := config.Duration("SWEEP_EVERY", time.Minute)
	if err != nil {
		// Unreadable rather than absent: fall back to the default and say so,
		// because a typo here would otherwise silently stop auto-confirmation.
		d.Log.Error("SWEEP_EVERY unusable; using the default", "error", err, "every", every)
	}
	if every > 0 && d.DB != nil {
		d.Log.Info("auto-confirm sweep scheduled", "every", every)
		go h.sweepLoop(d.Ctx, every)
	}
	// Service twin (#102): verification reads contest standing mid-verdict.
	// Credentials, the status list and the issuer key are verification's own
	// since #137 — this service answers questions about windows and disputes.
	mux.HandleFunc("GET /internal/contests", h.contests)
	mux.HandleFunc("GET /v1/unreleased", h.unreleased)
	mux.HandleFunc("GET /v1/unreached", h.unreached)
	mux.HandleFunc("POST /v1/claims/{claimId}/assist", h.assist)
}

type windowHandlers struct {
	d      service.Deps
	window time.Duration
	ex     *exiter
}

// openWindow is called by evidence's outbox when a claim is created.
//
// Opening the window and queueing the notification happen together: a window
// nobody was told about is a worker who cannot confirm and cannot dispute,
// which is W2 broken quietly.
func (h *windowHandlers) openWindow(w http.ResponseWriter, r *http.Request) {
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
		// Notifications are dropped (#150): the window opens with nothing
		// enqueued and notified_at unset, and the reach column stays NULL —
		// the truthful reading, "nobody claimed the worker was told". The
		// enqueue returns here when a channel exists again; the recorded gap
		// is that until then a worker learns about a window only by opening
		// the app.
		return nil
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

func (h *windowHandlers) getWindow(w http.ResponseWriter, r *http.Request) {
	win, err := getWindow(r.Context(), h.d.DB.Q(), r.PathValue("claimId"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "window", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, win)
}

// confirm is the worker saying yes, or a supervisor saying it for them.
//
// Both are legitimate and only one of them used to be distinguishable. Before
// #89, `route` was a string in the body: a request could say "self" while being
// anybody at all, and the record would then read as the worker's own
// confirmation of work they had never been asked about. The route is now
// derived from who the caller proved to be, and a body that contradicts it is
// refused rather than believed — because the whole value of recording an
// assisted confirmation as assisted is that somebody's name is on it.
func (h *windowHandlers) confirm(w http.ResponseWriter, r *http.Request) {
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

	win, ok := h.windowFor(w, r)
	if !ok {
		return
	}
	// Confirming is acting as the worker whose claim it is. Assisted reaches
	// here through X-CREST-On-Behalf-Of plus the act-for-party authorization,
	// which is what makes a supervisor confirming for a worker with no phone
	// an ordinary recorded action rather than a hole.
	if _, ok := identity.Authorize(w, r, h.d.Log, win.PartyID, win.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	if caller := identity.From(r.Context()); caller.Authenticated() {
		actual := routeSelf
		if caller.Assisting() {
			actual = routeAssisted
		}
		if body.Route != "" && body.Route != actual {
			httpx.WriteError(w, http.StatusBadRequest, "route_contradicts_caller",
				"this request says %q and the caller proved %q; the exit route is what happened, not what was asked for",
				body.Route, actual)
			return
		}
		route = actual
	}
	h.finish(w, r, route)
}

// listWindows is a worker's whole confirmation history in one read.
//
// It requires a partyId. A list of every window in the deployment is not a
// worker's record, it is a report, and serving one from the endpoint a worker's
// own client calls is how a bulk export gets built by accident.
func (h *windowHandlers) listWindows(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("partyId") == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"partyId is required: this endpoint answers what happened to one worker's claims")
		return
	}
	// One worker's history: the worker, or somebody acting for them (#102).
	// Expanded before checking so a stale bookmark naming an absorbed id is
	// still the survivor's own history (#100).
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, ids[0], "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	windows, err := windowsFor(r.Context(), h.d.DB.Q(), ids)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list windows", err)
		return
	}
	if windows == nil {
		windows = []Window{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"windows": windows, "count": len(windows)})
}

// windowFor loads the confirmation window a request is about, answering 404
// itself. Loaded before the action so the party to check against comes from
// the record rather than from the request.
func (h *windowHandlers) windowFor(w http.ResponseWriter, r *http.Request) (Window, bool) {
	win, err := getWindow(r.Context(), h.d.DB.Q(), r.PathValue("claimId"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "window", err, store.ErrNotFound)
		return Window{}, false
	}
	return win, true
}

// dispute contests the record. It does not contest the money: the release below
// is the same release every other exit makes (W4).
func (h *windowHandlers) dispute(w http.ResponseWriter, r *http.Request) {
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

	win, ok := h.windowFor(w, r)
	if !ok {
		return
	}
	// A dispute is checked differently from a confirmation, and the difference
	// is not an oversight. Confirming is acting *as* the worker; raising a
	// dispute is acting *as yourself* about somebody's record, and a
	// supervisor or a programme officer contesting a claim is a legitimate
	// thing that has nothing to do with acting on the worker's behalf. So what
	// is proved here is that the raiser is who the dispute says raised it —
	// which is the whole of what a dispute needs to be answerable later.
	raisedBy, ok := identity.Authorize(w, r, h.d.Log, body.RaisedByPartyID, win.ContextID,
		h.d.Authenticating, h.d.Permits)
	if !ok {
		return
	}
	body.RaisedByPartyID = raisedBy

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

// contests reports where any dispute against one target stands (#58).
//
// A dispute never revokes the credential it concerns. A credential is a
// historical statement — what was asserted, by whom, and when — and revoking it
// because the worker later objected would mean a worker who disputes one detail
// loses the whole record of work they did do. That is a penalty for objecting,
// and it falls on the person the dispute exists to protect.
//
// So the credential stands and the dispute is visible beside it. This endpoint
// is that visibility. It returns standing only: never the reason, never who
// raised it.
func (h *windowHandlers) contests(w http.ResponseWriter, r *http.Request) {
	kind, target := r.URL.Query().Get("targetKind"), r.URL.Query().Get("targetId")
	if kind == "" || target == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query",
			"targetKind and targetId are both required; an unscoped listing is a search for disputed workers")
		return
	}
	switch kind {
	case "claim", "credential", "linked-record":
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query",
			"targetKind is claim, credential or linked-record — never unit, because a contest against a Unit is not expressible (W5)")
		return
	}
	standing, err := contestsAgainst(r.Context(), h.d.DB.Q(), kind, target)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read contests", err)
		return
	}
	if standing == nil {
		standing = []ContestStanding{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contests": standing, "count": len(standing)})
}

// sweep auto-confirms every window whose time has run out.
//
// Driven by the clock and nothing else, and exposed as an endpoint so the
// harness can advance seven days and then ask for the consequences, rather than
// waiting for a ticker it cannot see.
func (h *windowHandlers) sweep(w http.ResponseWriter, r *http.Request) {
	now, due, swept, waiting, err := h.sweepOnce(r.Context())
	if err != nil {
		httpx.Fail(w, h.d.Log, "sweep", err)
		return
	}
	// due is what WAS owed, not what succeeded: a shortfall between due and
	// autoConfirmed + heldForSomeoneToLookAt is how a failed exit — a payment
	// attempted and left unreleased — stays visible in this response.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"at": now, "due": due, "autoConfirmed": swept,
		"heldForSomeoneToLookAt": waiting,
	})
}

// sweepOnce is one pass: auto-confirm everything due, and name everything due
// that it deliberately did not touch.
func (h *windowHandlers) sweepOnce(ctx context.Context) (time.Time, int, []string, []string, error) {
	now := h.d.Clock.Now()
	due, err := dueWindows(ctx, h.d.DB.Q(), now, 500)
	if err != nil {
		return now, 0, nil, nil, fmt.Errorf("find due windows: %w", err)
	}
	swept := make([]string, 0, len(due))
	for _, win := range due {
		if _, err := h.ex.exit(ctx, win.ClaimID, routeAuto); err != nil {
			// One claim failing must not stop the rest: the others are also
			// owed a payment, and a sweep that aborts halfway leaves the
			// remainder silently unpaid.
			h.d.Log.Error("auto-confirm failed", "claim", win.ClaimID, "error", err)
			continue
		}
		swept = append(swept, win.ClaimID)
	}
	// Windows whose worker was never reached are due and deliberately not
	// swept. They are reported so a sweep can never look like it did nothing
	// when in fact it declined to act on someone's behalf.
	unreached, err := unreachedWindows(ctx, h.d.DB.Q(), now)
	if err != nil {
		return now, len(due), swept, nil, fmt.Errorf("find unreached windows: %w", err)
	}
	waiting := make([]string, 0, len(unreached))
	for _, win := range unreached {
		waiting = append(waiting, win.ClaimID)
	}
	if len(waiting) > 0 {
		h.d.Log.Warn("windows past T=7 whose worker was never reached; not auto-confirming",
			"count", len(waiting))
	}
	// The count of everything owed — including a claim whose exit failed and
	// so appears in neither slice. The unreached windows are due too; they
	// were just never in dueWindows' answer, which excludes them.
	return now, len(due) + len(waiting), swept, waiting, nil
}

// sweepLoop is the sweep nobody has to remember to call.
//
// The endpoint alone was enough for the harness, which advances seven days and
// then asks for the consequences. It is not enough for a deployment: with only
// the endpoint, a window that reaches T=7 on a running stack stays open
// forever and the payment it owes is never released. "Every T=7 exit releases
// payment" is not a property of the exit routes alone — auto-confirm is one of
// the four exits, and it is the only one no person triggers.
func (h *windowHandlers) sweepLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every) //nolint:forbidigo // a ticker is elapsed time, not a reading of the clock
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, due, swept, waiting, err := h.sweepOnce(ctx)
			if err != nil && ctx.Err() == nil {
				h.d.Log.Error("scheduled sweep failed", "error", err)
				continue
			}
			if failed := due - len(swept) - len(waiting); failed > 0 {
				h.d.Log.Error("scheduled sweep left due payments unreleased",
					"due", due, "failed", failed)
			}
			if due > 0 {
				h.d.Log.Info("scheduled sweep", "due", due,
					"autoConfirmed", len(swept), "heldForSomeoneToLookAt", len(waiting))
			}
		}
	}
}

// assist is the supervisor-assisted route: a person confirming on behalf of a
// worker who could not be reached (§9).
//
// It is the answer to an unreached window, and it is a person's decision rather
// than a timer's. That is the whole difference: auto-confirmation on a worker
// who never heard is silence the system manufactured, and this is somebody
// taking responsibility for saying the record is true.
func (h *windowHandlers) assist(w http.ResponseWriter, r *http.Request) {
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
func (h *windowHandlers) unreached(w http.ResponseWriter, r *http.Request) {
	// An operations list over other people's payments and windows: signed-in
	// callers (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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

func (h *windowHandlers) finish(w http.ResponseWriter, r *http.Request, route string) {
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

// unreleased should always answer zero. It exists so W4 can be checked rather
// than believed.
func (h *windowHandlers) unreleased(w http.ResponseWriter, r *http.Request) {
	// An operations list over other people's payments and windows: signed-in
	// callers (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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
