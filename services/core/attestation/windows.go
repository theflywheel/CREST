package attestation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/idempotency"
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
		d:            d,
		window:       window,
		supportOwner: config.Str("SUPPORT_OWNER_PARTY_ID", config.Str("CREST_OPERATOR_PARTY_ID", "")),
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

	// Window creation is an internal handoff from evidence. It carries the
	// claim's authoritative party and must never be a public write that a
	// caller can use to create a payment window for somebody else.
	mux.HandleFunc("POST /internal/windows", h.openWindow)
	mux.HandleFunc("GET /v1/windows/{claimId}", h.getWindow)
	// A worker's confirmation history, following any merge (#100).
	mux.HandleFunc("GET /v1/windows", h.listWindows)
	mux.HandleFunc("POST /v1/claims/{claimId}/confirm", h.confirm)
	mux.HandleFunc("POST /v1/claims/{claimId}/dispute", h.dispute)
	mux.HandleFunc("GET /v1/contests", h.contests)
	mux.HandleFunc("GET /v1/claims/{claimId}/contest", h.claimOutcome)
	mux.HandleFunc("POST /v1/contests/{contestId}/decide", h.decideContest)
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
	mux.HandleFunc("POST /v1/windows/{claimId}/ack", h.acknowledge)
	mux.HandleFunc("POST /v1/windows/{claimId}/ack/assist", h.assistedAcknowledge)
	// Delivery systems report the outcome of actually notifying a worker. A
	// queued message is not evidence of reach, so NULL remains ineligible for
	// the auto-confirm sweep until this callback records a result.
	mux.HandleFunc("POST /internal/windows/{claimId}/reach", h.reach)
}

type windowHandlers struct {
	d            service.Deps
	window       time.Duration
	supportOwner string
	ex           *exiter
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
	token, err := newReviewToken()
	if err != nil {
		httpx.Fail(w, h.d.Log, "create review token", err)
		return
	}
	digest := sha256.Sum256([]byte(token))
	digestText := base64.RawURLEncoding.EncodeToString(digest[:])
	win.reviewTokenHash = &digestText
	notice := claimNotification{
		ClaimID: win.ClaimID, PartyID: win.PartyID, ContextID: win.ContextID,
		ClosesAt: win.ClosesAt, AcknowledgementToken: token,
	}

	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		created, err := insertWindow(r.Context(), tx, win)
		if err != nil || !created {
			return err // a redelivery: the window already exists, and one is enough
		}
		return store.Enqueue(r.Context(), tx, topicNotifyClaim, notice)
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

type claimNotification struct {
	ClaimID              string    `json:"claimId"`
	PartyID              string    `json:"partyId"`
	ContextID            string    `json:"contextId"`
	ClosesAt             time.Time `json:"closesAt"`
	AcknowledgementToken string    `json:"ackToken"`
}

func newReviewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (h *windowHandlers) getWindow(w http.ResponseWriter, r *http.Request) {
	win, err := getWindow(r.Context(), h.d.DB.Q(), r.PathValue("claimId"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "window", err, store.ErrNotFound)
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, win.PartyID, win.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
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
	if !authorizeWindowDetails(w, r, h.d, win) {
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
	raw, ok := readDisputeJSON(w, r, &body)
	if !ok {
		return
	}
	key, ok := requireDisputeIdempotencyKey(w, r)
	if !ok {
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
	// A dispute is raised by the authenticated caller, never by the party named
	// in a body field. A worker may dispute their own claim. An authorized agent
	// may raise one while acting for the worker, but the contest still records
	// the agent as its raiser so the audit trail cannot turn assistance into
	// fabricated worker authorship. Another direct reviewer needs the
	// project-scoped review function.
	caller := identity.From(r.Context())
	if !caller.Authenticated() || caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "dispute_raiser_not_proven",
			"a dispute needs an authenticated, enrolled actor")
		return
	}
	raisedBy := caller.PartyID
	if caller.Assisting() {
		if _, ok := identity.Authorize(w, r, h.d.Log, win.PartyID, win.ContextID,
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	} else {
		if _, ok := identity.Authorize(w, r, h.d.Log, caller.PartyID, win.ContextID,
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
		if raisedBy != win.PartyID && !authorizeReviewOperations(w, r, h.d, win.ContextID) {
			return
		}
	}
	if body.RaisedByPartyID != "" && body.RaisedByPartyID != raisedBy {
		httpx.WriteError(w, http.StatusForbidden, "raiser_contradicts_caller",
			"raisedByPartyId must identify the authenticated caller")
		return
	}

	claimID := r.PathValue("claimId")
	contest := schema.Contest{
		ID:              id.New(h.d.Clock, "contest"),
		Target:          schema.ContestTarget{Kind: schema.ContestTargetKindClaim, ID: claimID},
		RaisedByPartyID: raisedBy,
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
		reservation, err := beginDisputeIdempotency(r.Context(), tx, r, key, raisedBy, raw)
		if err != nil {
			return err
		}
		if reservation.Replay() {
			result := reservation.Result()
			if result.ResourceType != "contest" || result.ResourceID == "" {
				return fmt.Errorf("dispute idempotency record has no contest resource")
			}
			return nil
		}
		if err := insertContest(r.Context(), tx, contest); err != nil {
			return err
		}
		return reservation.Complete(r.Context(), tx, idempotency.Result{
			Status: http.StatusOK, ResourceType: "contest", ResourceID: contest.ID,
		})
	}); err != nil {
		if errors.Is(err, idempotency.ErrInvalidRequest) || errors.Is(err, idempotency.ErrFingerprint) || errors.Is(err, idempotency.ErrInProgress) {
			writeDisputeIdempotencyError(w, h.d.Log, err)
			return
		}
		httpx.Fail(w, h.d.Log, "record dispute", err)
		return
	}
	// The contest and its idempotency reservation are durable before the exit
	// work begins. If evidence/payment work fails, the same key retries this
	// contest instead of creating a second OPEN contest.
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

func (h *windowHandlers) claimOutcome(w http.ResponseWriter, r *http.Request) {
	win, ok := h.windowFor(w, r)
	if !ok {
		return
	}
	// The outcome includes free-text dispute reasons, raiser identity and
	// evidence. It is private to the worker and authorized review staff; the
	// public contests projection above intentionally exposes standing only.
	if !authorizeWindowDetails(w, r, h.d, win) {
		return
	}
	contests, err := contestsForClaim(r.Context(), h.d.DB.Q(), win.ClaimID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read claim contests", err)
		return
	}
	for i := range contests {
		contests[i].Decisions, err = contestDecisions(r.Context(), h.d.DB.Q(), contests[i].Contest.ID)
		if err != nil {
			httpx.Fail(w, h.d.Log, "read contest decisions", err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contests": contests})
}

func (h *windowHandlers) decideContest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision       string `json:"decision"`
		Reason         string `json:"reason"`
		Evidence       string `json:"evidence"`
		ReplacementRef string `json:"replacementRef"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if (body.Decision != "UPHELD" && body.Decision != "CORRECTED" && body.Decision != "REJECTED") ||
		body.Reason == "" || body.Evidence == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"decision, reason and evidence are required; decision is UPHELD, CORRECTED or REJECTED")
		return
	}
	contest, err := getContest(r.Context(), h.d.DB.Q(), r.PathValue("contestId"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "contest", err, store.ErrNotFound)
		return
	}
	if contest.Target.Kind != schema.ContestTargetKindClaim {
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_target",
			"payment contest decisions currently require a claim target")
		return
	}
	win, err := getWindow(r.Context(), h.d.DB.Q(), contest.Target.ID, false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "window", err, store.ErrNotFound)
		return
	}
	if !authorizeReviewOperations(w, r, h.d, win.ContextID) {
		return
	}
	reviewer := identity.From(r.Context()).PartyID
	if reviewer == contest.RaisedByPartyID {
		httpx.WriteError(w, http.StatusForbidden, "independent_reviewer_required", "the person raising the dispute cannot decide it")
		return
	}
	if body.Decision == "CORRECTED" {
		if err := validateReplacement(r.Context(), h.ex.verification, h.ex.evidence, win, body.ReplacementRef); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_replacement", "%v", err)
			return
		}
	}
	decision := ContestDecision{
		ID: id.New(h.d.Clock, "contest-decision"), ContestID: contest.ID,
		Decision: body.Decision, DecidedBy: reviewer, Reason: body.Reason,
		Evidence: body.Evidence, DecidedAt: h.d.Clock.Now(),
	}
	var correction *CorrectionEvent
	if body.Decision == "CORRECTED" {
		credentialID := win.CredentialID
		correction = &CorrectionEvent{
			ID: id.New(h.d.Clock, "correction-event"), ContestID: contest.ID,
			DecisionID: decision.ID, ClaimID: contest.Target.ID,
			CredentialID: credentialID, ReplacementRef: body.ReplacementRef,
			Reason: body.Reason, Evidence: body.Evidence, EmittedAt: decision.DecidedAt,
		}
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if err := insertContestDecision(r.Context(), tx, decision); err != nil {
			return err
		}
		if correction != nil {
			if err := insertCorrectionEvent(r.Context(), tx, *correction); err != nil {
				return err
			}
		}
		return store.Enqueue(r.Context(), tx, topicContestResolution, resolutionEvent{Decision: decision, Window: win, ReplacementRef: body.ReplacementRef})
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record contest decision", err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"decision": decision, "correction": correction})
}

// sweep auto-confirms every window whose time has run out.
//
// Driven by the clock and nothing else, and exposed as an endpoint so the
// harness can advance seven days and then ask for the consequences, rather than
// waiting for a ticker it cannot see.
func (h *windowHandlers) sweep(w http.ResponseWriter, r *http.Request) {
	contextID := r.URL.Query().Get("contextId")
	if !authorizeReviewOperations(w, r, h.d, contextID) {
		return
	}
	now, due, swept, waiting, err := h.sweepOnce(r.Context(), contextID)
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
func (h *windowHandlers) sweepOnce(ctx context.Context, contextID string) (time.Time, int, []string, []string, error) {
	now := h.d.Clock.Now()
	due, err := dueWindows(ctx, h.d.DB.Q(), now, contextID, 500)
	if err != nil {
		return now, 0, nil, nil, fmt.Errorf("find due windows: %w", err)
	}
	swept := make([]string, 0, len(due))
	dueCount := len(due)
	for _, win := range due {
		if _, err := h.ex.exit(ctx, win.ClaimID, routeAuto); err != nil {
			if errors.Is(err, errAutoNotEligible) {
				// The row changed after candidate selection. It is no longer
				// eligible under the lock, so leave it for the next pass.
				dueCount--
				continue
			}
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
	unreached, err := unreachedWindows(ctx, h.d.DB.Q(), now, contextID)
	if err != nil {
		return now, dueCount, swept, nil, fmt.Errorf("find unreached windows: %w", err)
	}
	waiting := make([]string, 0, len(unreached))
	for _, win := range unreached {
		if win.EscalatedAt == nil {
			if err := h.d.DB.InTx(ctx, func(tx store.Querier) error {
				return markEscalated(ctx, tx, win.ClaimID, h.supportOwner,
					"worker notification has no positive acknowledgement; support must resolve access", now)
			}); err != nil {
				return now, dueCount, swept, waiting, fmt.Errorf("assign support task: %w", err)
			}
		}
		waiting = append(waiting, win.ClaimID)
	}
	if len(waiting) > 0 {
		h.d.Log.Warn("windows past T=7 whose worker was never reached; not auto-confirming",
			"count", len(waiting))
	}
	// The count of everything owed — including a claim whose exit failed and
	// so appears in neither slice. The unreached windows are due too; they
	// were just never in dueWindows' answer, which excludes them.
	return now, dueCount + len(waiting), swept, waiting, nil
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
			_, due, swept, waiting, err := h.sweepOnce(ctx, "")
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
	win, ok := h.windowFor(w, r)
	if !ok {
		return
	}
	// The worker is the party being acted for; the assistant must be the
	// authenticated caller. Authorize checks the X-CREST-On-Behalf-Of target
	// against the worker and requires the act-for-party permit in this context.
	if _, ok := identity.Authorize(w, r, h.d.Log, win.PartyID, win.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	caller := identity.From(r.Context())
	if !caller.Authenticated() || caller.PartyID != body.AssistedByPartyID ||
		!caller.Assisting() || caller.RequestedFor() != win.PartyID {
		httpx.WriteError(w, http.StatusForbidden, "assistant_not_proven",
			"assistedByPartyId must be the authenticated supervisor acting for this worker")
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return markEscalated(r.Context(), tx, claimID, h.supportOwner,
			"worker requires assisted confirmation", h.d.Clock.Now())
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record escalation", err)
		return
	}
	h.finish(w, r, routeAssisted)
}

// reach is the delivery callback contract. The notification adapter calls it
// after it has a concrete delivery result, with reached when the worker was
// actually told and unreached when delivery failed. Repeated identical
// callbacks are idempotent. Once a positive delivery is recorded it cannot
// be downgraded by a later failed channel attempt; an explicit unreached
// result may be upgraded if a retry actually reaches the worker.
func (h *windowHandlers) reach(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reach  string `json:"reach"`
		Detail string `json:"detail"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.Reach != "unreached" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_reach",
			"provider callbacks may record unreached only; reached requires authenticated worker acknowledgement")
		return
	}
	claimID := r.PathValue("claimId")
	var win Window
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		win, err = getWindow(r.Context(), tx, claimID, true)
		if err != nil {
			return err
		}
		if win.Reach != nil && *win.Reach == "reached" && body.Reach != "reached" {
			return fmt.Errorf("reach already recorded as %s", *win.Reach)
		}
		return recordReach(r.Context(), tx, claimID, body.Reach, body.Detail, h.d.Clock.Now())
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no window for that claim")
	case err != nil:
		httpx.WriteError(w, http.StatusConflict, "reach_conflict", "%v", err)
	default:
		if latest, readErr := getWindow(r.Context(), h.d.DB.Q(), claimID, false); readErr == nil {
			win = latest
		}
		httpx.WriteJSON(w, http.StatusOK, win)
	}
}

func (h *windowHandlers) acknowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if r.ContentLength > 0 {
		if !httpx.ReadJSON(w, r, &body) {
			return
		}
	}
	if body.Token == "" {
		body.Token = r.URL.Query().Get("token")
	}
	win, ok := h.windowFor(w, r)
	if !ok || !validReviewToken(win, body.Token) {
		if ok {
			httpx.WriteError(w, http.StatusForbidden, "invalid_acknowledgement",
				"this acknowledgement link is invalid or expired")
		}
		return
	}
	caller := identity.From(r.Context())
	if !caller.Authenticated() || caller.Assisting() {
		httpx.WriteError(w, http.StatusForbidden, "worker_acknowledgement_required",
			"the worker must acknowledge this review link as themselves")
		return
	}
	if caller.PartyID != win.PartyID {
		httpx.WriteError(w, http.StatusForbidden, "worker_not_proven",
			"the authenticated caller is not this worker")
		return
	}
	updated, err := h.recordAcknowledgement(r.Context(), win.ClaimID, caller.PartyID, "worker acknowledged the review link", "")
	if err != nil {
		httpx.Fail(w, h.d.Log, "record acknowledgement", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (h *windowHandlers) assistedAcknowledge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason   string `json:"reason"`
		Evidence string `json:"evidence"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.Reason == "" || body.Evidence == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"assisted acknowledgement needs a reason and evidence")
		return
	}
	win, ok := h.windowFor(w, r)
	if !ok {
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, win.PartyID, win.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	caller := identity.From(r.Context())
	if !caller.Authenticated() || !caller.Assisting() || caller.RequestedFor() != win.PartyID {
		httpx.WriteError(w, http.StatusForbidden, "assistant_not_proven",
			"an assisted acknowledgement needs an authorized agent acting for the worker")
		return
	}
	updated, err := h.recordAcknowledgement(r.Context(), win.ClaimID, caller.PartyID, body.Reason, body.Evidence)
	if err != nil {
		httpx.Fail(w, h.d.Log, "record assisted acknowledgement", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func validReviewToken(win Window, token string) bool {
	if win.reviewTokenHash == nil || token == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:]) == *win.reviewTokenHash
}

func (h *windowHandlers) recordAcknowledgement(ctx context.Context, claimID, by, reason, evidence string) (Window, error) {
	var out Window
	err := h.d.DB.InTx(ctx, func(tx store.Querier) error {
		win, err := getWindow(ctx, tx, claimID, true)
		if err != nil {
			return err
		}
		if win.ExitRoute != nil {
			out = win
			return nil
		}
		if win.AcknowledgedAt != nil {
			out = win
			return nil
		}
		at := h.d.Clock.Now()
		if err := recordAcknowledgement(ctx, tx, claimID, at, by, reason, evidence, at.Add(h.window)); err != nil {
			return err
		}
		win.Reach, win.ReachDetail = stringPtr("reached"), stringPtr(reason)
		win.ReviewStartedAt, win.AcknowledgedAt, win.AcknowledgedBy = &at, &at, &by
		win.AcknowledgementReason, win.AcknowledgementEvidence = &reason, &evidence
		win.ClosesAt = at.Add(h.window)
		out = win
		return nil
	})
	return out, err
}

func stringPtr(v string) *string { return &v }

// unreached is the counterpart to /v1/unreleased: windows past T=7 without a
// positive delivery result, waiting for a person. Both exist so that a promise
// is a query rather than a hope.
func (h *windowHandlers) unreached(w http.ResponseWriter, r *http.Request) {
	// An operations list over other people's payments and windows: signed-in
	// callers (#102).
	if !authorizeReviewOperations(w, r, h.d, r.URL.Query().Get("contextId")) {
		return
	}
	rows, err := unreachedWindows(r.Context(), h.d.DB.Q(), h.d.Clock.Now(), r.URL.Query().Get("contextId"))
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
	if !authorizeReviewOperations(w, r, h.d, r.URL.Query().Get("contextId")) {
		return
	}
	rows, err := unreleased(r.Context(), h.d.DB.Q(), r.URL.Query().Get("contextId"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list unreleased", err)
		return
	}
	if rows == nil {
		rows = []Window{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"windows": rows, "count": len(rows)})
}
