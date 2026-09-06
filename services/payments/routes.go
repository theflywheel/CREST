package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/services/payments/providers"
)

func routes(mux *http.ServeMux, d service.Deps) {
	h := &handlers{
		d:              d,
		evidence:       client.New(config.Str("EVIDENCE_URL", "http://evidence:8080")),
		definitions:    client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
		definitionsURL: config.Str("DEFINITIONS_URL", "http://definitions:8080"),
		rail:           mustConfiguredProvider(d),
		payerOwner:     config.Str("HELD_PAYMENT_OWNER", "did:crest:party:programme-operations"),
	}
	if simulator, ok := h.rail.(*providers.Simulator); ok {
		h.simulator = simulator
		mux.HandleFunc("POST /v1/providers/simulator/settle", h.settleSimulator)
	}

	// Releasing an instruction is the confirmation service's exit speaking —
	// internal only (#102, service-identity ruling). Nothing human calls it:
	// a person who could release payments by POST would be a payment path
	// with no T=7 exit behind it.
	mux.HandleFunc("POST /internal/instructions", h.release)
	mux.HandleFunc("GET /v1/instructions", h.list)
	// Capability read (#102): the claim id names one instruction, the same
	// judgement as record reads on evidence.
	mux.HandleFunc("GET /v1/instructions/by-claim/{claimId}", h.byClaim)
	mux.HandleFunc("POST /v1/instructions/{id}/retry", h.retryHeld)
	mux.HandleFunc("POST /internal/held/retry", h.retryHeldAll)
	mux.HandleFunc("GET /v1/reconciliation", h.reconciliation)

	// The funders wave (F-1, F-2): rate ownership, rates as terms, and the
	// mechanism whose activation gate sits in front of disbursement only.
	fundersRoutes(mux, d, h)
	// Dependency holds are retried by the service itself. Otherwise a rate or
	// evidence outage at the exact release moment would become a permanent
	// HELD record that nobody can clear after the dependency recovers.
	every, err := config.Duration("HELD_RETRY_EVERY", time.Minute)
	if err != nil {
		d.Log.Error("HELD_RETRY_EVERY unusable; using the default", "error", err, "every", every)
	}
	if every > 0 && d.DB != nil && d.Ctx != nil {
		go h.heldRetryLoop(d.Ctx, every)
	}
}

type handlers struct {
	d           service.Deps
	evidence    *client.Client
	definitions *client.Client
	// A funder mutation is initiated by a verified human caller. The public
	// linked-record endpoint must receive that caller's bearer token; the
	// service client's internal headers alone cannot establish who authored a
	// rate.
	definitionsURL string
	rail           providers.Provider
	simulator      *providers.Simulator
	payerOwner     string
}

func (h *handlers) settleSimulator(w http.ResponseWriter, r *http.Request) {
	if h.simulator == nil || (h.d.Config.Env != "local" && h.d.Config.Env != "development") {
		httpx.WriteError(w, http.StatusNotFound, "provider_unavailable", "the development simulator is not enabled")
		return
	}
	var body struct {
		ContextID      string `json:"contextId"`
		IdempotencyKey string `json:"idempotencyKey"`
		Reference      string `json:"reference"`
		AmountMinor    int64  `json:"amountMinor"`
		Currency       string `json:"currency"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if !authorizePaymentOperations(w, r, h.d, body.ContextID) {
		return
	}
	settled, err := h.simulator.Settle(r.Context(), body.ContextID, body.IdempotencyKey, body.Reference, body.AmountMinor, body.Currency)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, "settlement_refused", "%v", err)
		return
	}
	// Settle changes the provider's durable state. Re-submit the associated
	// instruction through the normal consumer so the payments service records
	// the same terminal result in compensations. This is idempotent: the
	// simulator returns its already-confirmed row on every replay.
	in, err := getInstructionByID(r.Context(), h.d.DB.Q(), settled.InstructionID, false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "instruction", err, store.ErrNotFound)
		return
	}
	payload, err := json.Marshal(in)
	if err != nil {
		httpx.Fail(w, h.d.Log, "encode settled instruction", err)
		return
	}
	if err := sendToRail(r.Context(), h.d, h.rail, payload); err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "settlement_recording_failed",
			"the provider settled the transfer but payments could not record it: %v", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settled)
}

type railReply = providers.Response

// release turns a T=7 exit into an instruction.
//
// Note what it does not do: look at which exit it was. All four release
// (W4) — including dispute, because a dispute contests the record and not the
// money. `releasedBy` is recorded for reconciliation, never branched on.
func (h *handlers) release(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClaimID    string    `json:"claimId"`
		UnitID     string    `json:"unitId"`
		PartyID    string    `json:"partyId"`
		ContextID  string    `json:"contextId"`
		ReleasedBy string    `json:"releasedBy"`
		ReleasedAt time.Time `json:"releasedAt"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.ClaimID == "" || req.UnitID == "" || req.PartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a release needs a claim, a unit and a payee")
		return
	}

	// Already instructed? Say so and stop. The relay is at-least-once, and the
	// unique index below is the real guarantee, but answering the same thing
	// twice is friendlier than answering an error.
	if existing, err := getInstructionByClaim(r.Context(), h.d.DB.Q(), req.ClaimID); err == nil {
		httpx.WriteJSON(w, http.StatusOK, existing)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, h.d.Log, "look up instruction", err)
		return
	}

	now := h.d.Clock.Now()
	in := Instruction{
		ID:         id.New(h.d.Clock, "payment-instruction"),
		ClaimID:    req.ClaimID,
		UnitID:     req.UnitID,
		PartyID:    req.PartyID,
		ContextID:  req.ContextID,
		ReleasedBy: req.ReleasedBy,
		ReleasedAt: req.ReleasedAt,
		CreatedAt:  now,
	}

	// Price at the work period start. The release may happen days later, but
	// the worker's completed work must never be repriced by a later publication.
	pricing := h.priceFor(r.Context(), req.UnitID)
	// The computed amount stays on the instruction even when it is held: for a
	// mechanism hold what is owed is known, only its sending waits. Rate holds
	// leave it zero because zero-priced is the thing they refuse to assert.
	in.AmountMinor, in.Currency = pricing.AmountMinor, pricing.Currency
	in.RateRecordID, in.RateVersion = pricing.RateRecordID, pricing.RateVersion
	if !pricing.PricingAt.IsZero() {
		in.PricingAt = &pricing.PricingAt
	}
	held := pricing.Held
	if held == nil {
		// f2_9, exactly: the mechanism's gate sits in front of DISBURSEMENT,
		// not in front of this instruction existing. The window exit already
		// released the obligation (W4) — the only question the gate answers
		// is whether the money moves now or is held with the mechanism's
		// owner named against it (W10). The amount is kept on the hold: what
		// is owed is known, only its sending waits.
		held = h.mechanismHold(r.Context(), req.ContextID)
	}
	switch {
	case held != nil:
		// The payment is held, and it is held *with an explanation attached*.
		// The alternative — dropping the release, or logging and returning
		// 200 — is a worker seeing nothing and being told nothing (W10).
		in.State = "HELD"
		in.Held = held
	default:
		in.State = "RELEASED"
	}

	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		created, err := insertInstruction(r.Context(), tx, in)
		if err != nil || !created {
			return err
		}
		if in.State != "RELEASED" {
			return nil
		}
		return store.Enqueue(r.Context(), tx, topicRailSend, in)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "record instruction", err)
		return
	}

	// A held payment is still a 201: the instruction exists, and it is the
	// thing that carries the reason. Returning an error here would leave the
	// hold unrecorded and the caller retrying forever.
	httpx.WriteJSON(w, http.StatusCreated, in)
}

// mechanismHold is f2_9's gate applied where it belongs: at disbursement.
// A context with no mechanism record predates this surface and is not gated;
// a context whose mechanism is not ACTIVE holds the money with the
// mechanism's owner named. A read failure holds too, with the deployment's
// payer owner — never a silent release past a gate that could not be read.
func (h *handlers) mechanismHold(ctx context.Context, contextID string) *HeldReason {
	if contextID == "" {
		return nil // released before instructions carried a context; not gated
	}
	m, err := mechanismByContext(ctx, h.d.DB.Q(), contextID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return holdForMechanism(nil)
	case err != nil:
		return &HeldReason{
			Code:         "mechanism_unreadable",
			Explanation:  "whether this project's payment mechanism is live could not be read, so the money is held rather than sent past an unreadable gate",
			OwnerPartyID: h.payerOwner,
		}
	}
	return holdForMechanism(&m)
}

type pricingResult struct {
	AmountMinor  int64
	Currency     string
	Held         *HeldReason
	RateRecordID string
	RateVersion  int
	PricingAt    time.Time
}

// amountFor is retained as the small three-value pricing surface used by
// callers that only need the amount. Pricing itself is pinned by priceFor to
// the Unit period start; the release time must never reprice work.
func (h *handlers) amountFor(ctx context.Context, unitID string, _ time.Time) (int64, string, *HeldReason) {
	p := h.priceFor(ctx, unitID)
	return p.AmountMinor, p.Currency, p.Held
}

// priceFor works out what is owed using the rate in force when the work period
// started. It also returns the exact LinkedRecord identity and pricing instant
// so an instruction can preserve its audit trail across every hold retry.
//
// A zero-amount instruction would satisfy every count and pay nobody, which is
// precisely the silent failure W10 exists to prevent.
func (h *handlers) priceFor(ctx context.Context, unitID string) pricingResult {
	var unit schema.Unit
	if err := h.evidence.Get(ctx, "/internal/units/"+url.PathEscape(unitID), &unit); err != nil {
		return pricingResult{Held: &HeldReason{
			Code:         "unit_unreadable",
			Explanation:  fmt.Sprintf("the work record %s could not be read, so the amount cannot be worked out", unitID),
			OwnerPartyID: h.payerOwner,
		}}
	}
	pricingAt := unit.Period.Start
	if pricingAt.IsZero() {
		return pricingResult{Held: &HeldReason{Code: "work_period_unreadable",
			Explanation:  "the work record has no period start, so no rate can be pinned to when the work happened",
			OwnerPartyID: h.payerOwner}}
	}

	var out struct {
		LinkedRecords []schema.LinkedRecord `json:"linkedRecords"`
	}
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s/linked-records?type=payment-setup",
		url.PathEscape(unit.Definition.ID)), &out)
	if err != nil {
		return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
			Code:         "rate_unreadable",
			Explanation:  "the rate for this work could not be read from the definitions service",
			OwnerPartyID: h.payerOwner,
		}}
	}
	if len(out.LinkedRecords) == 0 {
		// Entirely legitimate: a definition is complete and usable with no rate
		// attached, because recognition is a use of its own (§7). The worker
		// still gets a credential; there is simply nothing to pay, and they are
		// told that rather than left wondering.
		return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
			Code:         "no_rate_attached",
			Explanation:  "this work is recognised but no payment rate is attached to its definition",
			OwnerPartyID: h.payerOwner,
		}}
	}

	// A rate is versioned terms (f1_4): several versions can be attached, and
	// the one that prices this unit is the version in force at the work period
	// start — never the latest, which could reprice work already done.
	versions := make([]rateVersion, 0, len(out.LinkedRecords))
	for _, lr := range out.LinkedRecords {
		if strings.TrimSpace(lr.ID) == "" || lr.Version < 1 {
			return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
				Code:         "rate_unreadable",
				Explanation:  "a rate is missing its immutable LinkedRecord id or version, so it cannot be safely priced",
				OwnerPartyID: h.payerOwner,
			}}
		}
		var p schema.PaymentSetupLinkedRecordPayload
		raw, mErr := json.Marshal(lr.Payload)
		if mErr != nil || json.Unmarshal(raw, &p) != nil {
			return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
				Code:         "rate_unreadable",
				Explanation:  "a rate attached to this definition is not in a shape payments understands",
				OwnerPartyID: h.payerOwner,
			}}
		}
		versions = append(versions, rateVersion{ID: lr.ID, Version: lr.Version, Payload: p})
	}
	inForce, ok := rateInForceAt(versions, pricingAt)
	if !ok {
		return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
			Code:         "no_rate_in_force",
			Explanation:  "a rate is published for this work but none of its versions was in force when the work period started",
			OwnerPartyID: h.payerOwner,
		}}
	}
	setup := inForce.Payload
	owner := setup.PayerPartyID
	if owner == "" {
		owner = h.payerOwner
	}

	// Money in minor units, and the multiplication rounded once, explicitly.
	// A float that reaches an amount field is a rounding argument waiting to
	// happen in a reconciliation meeting.
	total := math.Round(unit.Outcome.Value * float64(setup.RatePerOutcomeUnit.AmountMinor))
	if total < 0 || total > math.MaxInt64 {
		return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
			Code:         "amount_out_of_range",
			Explanation:  "the amount this record works out to is not a payable number",
			OwnerPartyID: owner,
		}, RateRecordID: inForce.ID, RateVersion: inForce.Version}
	}
	if total == 0 {
		// The comment below this line used to be the only thing standing
		// between here and a zero-amount RELEASED instruction. An outcome of
		// zero is a legitimate accepted row — the schema allows it and the
		// adapter only rejects negatives — and multiplying it by any rate gives
		// nothing to pay.
		//
		// Released with a zero amount, it satisfies every count, moves no
		// money, and carries no explanation for the worker: the exact silent
		// failure a held payment with a reason exists to prevent. Held instead,
		// with a sentence a person can act on.
		return pricingResult{PricingAt: pricingAt, Held: &HeldReason{
			Code:         "nothing_to_pay",
			Explanation:  "this record's outcome is zero, so there is nothing to pay for it",
			OwnerPartyID: owner,
		}, RateRecordID: inForce.ID, RateVersion: inForce.Version}
	}
	return pricingResult{AmountMinor: int64(total), Currency: setup.RatePerOutcomeUnit.Currency,
		PricingAt: pricingAt, RateRecordID: inForce.ID, RateVersion: inForce.Version}
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	// Party-filtered: the worker's own money, answered to them or their actor
	// — expanded across any merge first, so the survivor owns the whole
	// history (#100). Unfiltered: a signed-in operations surface (#102).
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	contextID := r.URL.Query().Get("contextId")
	if r.URL.Query().Get("partyId") != "" {
		if _, ok := identity.Authorize(w, r, h.d.Log, ids[0], "",
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	} else {
		if !authorizePaymentOperations(w, r, h.d, contextID) {
			return
		}
	}
	instructions, err := listInstructions(r.Context(), h.d.DB.Q(), ids, r.URL.Query().Get("state"), contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list instructions", err)
		return
	}
	if instructions == nil {
		instructions = []Instruction{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"instructions": instructions})
}

// retryHeld re-evaluates one held obligation. A dependency outage while the
// instruction was created is a recoverable hold, not a terminal payment
// failure; the owner can ask for a retry after restoring the dependency.
func (h *handlers) retryHeld(w http.ResponseWriter, r *http.Request) {
	contextID := r.URL.Query().Get("contextId")
	if !authorizePaymentOperations(w, r, h.d, contextID) {
		return
	}
	in, err := getInstructionByID(r.Context(), h.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "instruction", err, store.ErrNotFound)
		return
	}
	if in.ContextID != contextID {
		httpx.WriteError(w, http.StatusForbidden, "context_mismatch",
			"the requested instruction is outside the authorized project")
		return
	}
	if in.Held == nil {
		httpx.WriteJSON(w, http.StatusOK, in)
		return
	}
	updated, err := h.reevaluateHeld(r.Context(), in.ID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "retry held instruction", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

// retryHeldAll is the service-only sweep used by the deployment scheduler.
// It is also useful for an operator's internal maintenance call, but never
// trusts a caller supplied party or amount. Each instruction is reloaded and
// locked before it can transition, so repeated sweeps are idempotent.
func (h *handlers) retryHeldAll(w http.ResponseWriter, r *http.Request) {
	rows, err := heldInstructions(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "find held instructions", err)
		return
	}
	released, remaining := []string{}, []string{}
	for _, in := range rows {
		updated, err := h.reevaluateHeld(r.Context(), in.ID)
		if err != nil {
			h.d.Log.Error("held payment retry failed", "instruction", in.ID, "error", err)
			remaining = append(remaining, in.ID)
			continue
		}
		if updated.State == "RELEASED" {
			released = append(released, updated.ID)
		} else {
			remaining = append(remaining, updated.ID)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"releasedInstructionIds": released, "stillHeldInstructionIds": remaining,
	})
}

func (h *handlers) reevaluateHeld(ctx context.Context, instructionID string) (Instruction, error) {
	var out Instruction
	err := h.d.DB.InTx(ctx, func(tx store.Querier) error {
		in, err := getInstructionByID(ctx, tx, instructionID, true)
		if err != nil {
			return err
		}
		if in.State != "HELD" {
			out = in
			return nil
		}
		// A priced hold is immutable. Only the mechanism activation gate may
		// clear it; re-reading rates here would silently reprice an obligation.
		if (in.Held == nil || in.Held.Code != "mechanism_not_live") && !canRepriceHeld(in) {
			out = in
			return nil
		}

		amount, currency := in.AmountMinor, in.Currency
		hold := in.Held
		if canRepriceHeld(in) {
			pricing := h.priceFor(ctx, in.UnitID)
			amount, currency, hold = pricing.AmountMinor, pricing.Currency, pricing.Held
			in.RateRecordID, in.RateVersion = pricing.RateRecordID, pricing.RateVersion
			if !pricing.PricingAt.IsZero() {
				in.PricingAt = &pricing.PricingAt
			}
		}
		if hold != nil && hold.Code == "mechanism_not_live" {
			hold = h.mechanismHold(ctx, in.ContextID)
		}
		if hold != nil {
			in.State, in.Held = "HELD", hold
			in.AmountMinor, in.Currency = amount, currency
		} else {
			in.State, in.Held = "RELEASED", nil
			in.AmountMinor, in.Currency = amount, currency
		}
		if err := releaseHeldInstructionAndEnqueue(ctx, tx, in); err != nil {
			return err
		}
		out = in
		return nil
	})
	return out, err
}

func retryableMissingRate(hold *HeldReason) bool {
	if hold == nil {
		return false
	}
	return hold.Code == "no_rate_attached" || hold.Code == "no_rate_in_force"
}

func canRepriceHeld(in Instruction) bool {
	return in.RateRecordID == "" && retryableMissingRate(in.Held)
}

func (h *handlers) heldRetryLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every) // elapsed scheduling interval; payment decisions use h.d.Clock
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rows, err := heldInstructions(ctx, h.d.DB.Q())
			if err != nil {
				h.d.Log.Error("held payment retry scan failed", "error", err)
				continue
			}
			for _, in := range rows {
				if _, err := h.reevaluateHeld(ctx, in.ID); err != nil && ctx.Err() == nil {
					h.d.Log.Error("held payment retry failed", "instruction", in.ID, "error", err)
				}
			}
		}
	}
}

func (h *handlers) byClaim(w http.ResponseWriter, r *http.Request) {
	in, err := getInstructionByClaim(r.Context(), h.d.DB.Q(), r.PathValue("claimId"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "instruction", err, store.ErrNotFound)
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, in.PartyID, in.ContextID,
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, in)
}

func (h *handlers) reconciliation(w http.ResponseWriter, r *http.Request) {
	// The money-vs-record gap, with reasons and owners: operations (#102).
	contextID := r.URL.Query().Get("contextId")
	if !authorizePaymentOperations(w, r, h.d, contextID) {
		return
	}
	gaps, err := reconcile(r.Context(), h.d.DB.Q(), contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "reconcile", err)
		return
	}
	if gaps == nil {
		gaps = []gap{}
	}
	// A gap with no reason is itself the finding. Reporting it separately means
	// "everything unpaid has an explanation" is a number someone can watch.
	unexplained := 0
	for _, g := range gaps {
		if g.Reason == nil {
			unexplained++
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"gaps": gaps, "count": len(gaps), "withoutAReason": unexplained,
	})
}

// sendToRail delivers one instruction and records what came back.
//
// The idempotency key is the instruction id, so a retry after a timeout cannot
// pay twice — which is the failure the mock rail exists to inject.
func sendToRail(ctx context.Context, d service.Deps, rail providers.Provider, payload json.RawMessage) error {
	var in Instruction
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}

	var reply railReply

	comp := Compensation{
		ID:            id.New(d.Clock, "compensation"),
		InstructionID: in.ID,
		UnitID:        in.UnitID,
		AmountMinor:   in.AmountMinor,
		Currency:      in.Currency,
		CreatedAt:     d.Clock.Now(),
	}

	reply, railErr := rail.Submit(ctx, providers.Request{
		IdempotencyKey: in.ID,
		InstructionID:  in.ID,
		ContextID:      in.ContextID,
		Reference:      in.ClaimID,
		AmountMinor:    in.AmountMinor,
		Currency:       in.Currency,
		Destination:    in.PartyID,
	})
	terminalErr := railErr
	if railErr != nil {
		// A non-2xx response can still contain the rail's terminal rejection
		// result (the local rail uses 422 for this). Decode that result before
		// treating transport errors as uncertain. A timeout or unavailable rail
		// stays SENT so the outbox retries instead of turning an outage into a
		// permanent FAILED payment.
		var status *providers.HTTPError
		if errors.As(railErr, &status) && status.Body != "" {
			_ = json.Unmarshal([]byte(status.Body), &reply)
		}
	}
	status := normalizeRailStatus(reply.Status)
	if status != "" && !railReplyMatchesInstruction(reply, in) {
		comp.State = "SENT"
		comp.Failure = &HeldReason{Code: "rail_instruction_mismatch",
			Explanation:  "the rail response did not identify the requested payment instruction",
			OwnerPartyID: in.PartyID}
		terminalErr = errors.New("rail response identified a different instruction")
		status = "mismatch"
	}
	// A confirmed body on a failed HTTP response is not a settlement. Preserve
	// transport failure as SENT so a later outbox attempt can ask the rail again.
	if railErr != nil && status == "confirmed" {
		comp.State = "SENT"
		comp.Failure = &HeldReason{Code: "rail_http_error",
			Explanation:  "the rail returned a confirmation body with a failed HTTP response, so settlement was not accepted",
			OwnerPartyID: in.PartyID}
		terminalErr = railErr
		status = "transport_error"
	}
	switch status {
	case "failed":
		comp.State = "FAILED"
		comp.Failure = &HeldReason{
			Code:         "rail_rejected",
			Explanation:  "the rail rejected this payment",
			OwnerPartyID: in.PartyID,
		}
		terminalErr = nil
	case "pending":
		comp.State = "SENT"
		comp.Failure = &HeldReason{
			Code:         "rail_pending",
			Explanation:  "the rail accepted this payment but has not settled it yet",
			OwnerPartyID: in.PartyID,
		}
		terminalErr = fmt.Errorf("rail payment %s is pending", in.ID)
	case "confirmed":
		comp.State, comp.RailRef = "CONFIRMED", &reply.Reference
		comp.ConfirmedAt = timePtr(d.Clock.Now())
		terminalErr = nil
		// A confirmation is only a truthful confirmation of the amount the
		// rail says it settled. Require that value and retain it in the
		// compensation; never infer settlement from the instruction alone.
		settledAmount, settledCurrency, ok := settledAmount(reply)
		if strings.TrimSpace(reply.Reference) == "" {
			comp.State, comp.RailRef, comp.ConfirmedAt = "SENT", nil, nil
			comp.Failure = &HeldReason{Code: "rail_reference_missing",
				Explanation:  "the rail confirmed without reporting a reconciliation reference",
				OwnerPartyID: in.PartyID}
			terminalErr = errors.New("rail confirmation omitted reference")
		} else if !ok {
			comp.State = "SENT"
			comp.RailRef, comp.ConfirmedAt = nil, nil
			comp.Failure = &HeldReason{Code: "rail_amount_missing",
				Explanation:  "the rail confirmed without reporting the settled amount",
				OwnerPartyID: in.PartyID}
			terminalErr = errors.New("rail confirmation omitted settled amount")
		} else {
			comp.AmountMinor, comp.Currency = settledAmount, settledCurrency
			if settledAmount != in.AmountMinor || settledCurrency != in.Currency {
				comp.State, comp.ConfirmedAt = "FAILED", nil
				comp.Failure = &HeldReason{Code: "settled_amount_mismatch",
					Explanation: fmt.Sprintf("the rail settled %d %s, but the instruction was %d %s",
						settledAmount, settledCurrency, in.AmountMinor, in.Currency),
					OwnerPartyID: in.PartyID}
				terminalErr = nil
			}
		}
	case "mismatch":
		// The response had a known status but named another transfer. The
		// mismatch reason set above is retained and the outbox retries safely.
	case "transport_error":
		// The failed HTTP response reason set above is retained.
	default:
		comp.State = "SENT"
		comp.Failure = &HeldReason{Code: "rail_status_unreadable",
			Explanation:  "the rail response did not contain a recognised settlement status",
			OwnerPartyID: in.PartyID}
		terminalErr = fmt.Errorf("unrecognised rail status %q", reply.Status)
	}
	if railErr != nil && status != "failed" {
		// Preserve transport uncertainty even when the body was empty or
		// malformed. An explicit terminal failed status above is the only case
		// where the rail error is safe to consume.
		if status != "pending" {
			terminalErr = railErr
		}
	}

	if err := d.DB.InTx(ctx, func(tx store.Querier) error {
		return upsertCompensation(ctx, tx, comp)
	}); err != nil {
		return err
	}
	// Returning the rail error keeps the message in the outbox for another
	// attempt. The rail is idempotent on the key, so retrying is safe, and a
	// payment that never went out must not be quietly marked delivered.
	return terminalErr
}

func railReplyMatchesInstruction(reply railReply, in Instruction) bool {
	want := strings.TrimSpace(in.ID)
	if want == "" {
		return false
	}
	return strings.TrimSpace(reply.IdempotencyKey) == want ||
		strings.TrimSpace(reply.InstructionID) == want ||
		strings.TrimSpace(reply.TransferID) == want
}

func normalizeRailStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "confirmed":
		return "confirmed"
	case "pending", "processing", "queued", "submitted", "sent":
		return "pending"
	case "failed", "rejected", "declined", "cancelled", "canceled", "error":
		return "failed"
	default:
		return ""
	}
}

func settledAmount(reply railReply) (int64, string, bool) {
	amount := reply.SettledAmountMinor
	if amount == nil {
		amount = reply.AmountMinor
	}
	currency := reply.SettledCurrency
	if currency == "" {
		currency = reply.Currency
	}
	if amount == nil || currency == "" {
		return 0, "", false
	}
	return *amount, currency, true
}

func timePtr(t time.Time) *time.Time { return &t }
