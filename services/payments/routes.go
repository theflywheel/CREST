package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
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

func routes(mux *http.ServeMux, d service.Deps) {
	h := &handlers{
		d:           d,
		evidence:    client.New(config.Str("EVIDENCE_URL", "http://evidence:8080")),
		definitions: client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
		rail:        client.New(config.Str("RAIL_URL", "http://mock-rail:8080")),
		payerOwner:  config.Str("HELD_PAYMENT_OWNER", "did:crest:party:programme-operations"),
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
	mux.HandleFunc("GET /v1/reconciliation", h.reconciliation)

	// The funders wave (F-1, F-2): rate ownership, rates as terms, and the
	// mechanism whose activation gate sits in front of disbursement only.
	fundersRoutes(mux, d, h)
}

type handlers struct {
	d           service.Deps
	evidence    *client.Client
	definitions *client.Client
	rail        *client.Client
	payerOwner  string
}

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

	// Priced at the release moment: the rate version in force when the window
	// exited, not whatever was published since (f1_4).
	amount, currency, held := h.amountFor(r.Context(), req.UnitID, req.ReleasedAt)
	// The computed amount stays on the instruction even when it is held: for a
	// mechanism hold what is owed is known, only its sending waits. Rate holds
	// leave it zero because zero-priced is the thing they refuse to assert.
	in.AmountMinor, in.Currency = amount, currency
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

// amountFor works out what is owed at a moment: the rate version in force at
// `at` multiplied by the unit's outcome. Anything missing is a hold with a
// reason, never a zero.
//
// A zero-amount instruction would satisfy every count and pay nobody, which is
// precisely the silent failure W10 exists to prevent.
func (h *handlers) amountFor(ctx context.Context, unitID string, at time.Time) (int64, string, *HeldReason) {
	var unit schema.Unit
	if err := h.evidence.Get(ctx, "/internal/units/"+url.PathEscape(unitID), &unit); err != nil {
		return 0, "", &HeldReason{
			Code:         "unit_unreadable",
			Explanation:  fmt.Sprintf("the work record %s could not be read, so the amount cannot be worked out", unitID),
			OwnerPartyID: h.payerOwner,
		}
	}

	var out struct {
		LinkedRecords []schema.LinkedRecord `json:"linkedRecords"`
	}
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s/linked-records?type=payment-setup",
		url.PathEscape(unit.Definition.ID)), &out)
	if err != nil {
		return 0, "", &HeldReason{
			Code:         "rate_unreadable",
			Explanation:  "the rate for this work could not be read from the definitions service",
			OwnerPartyID: h.payerOwner,
		}
	}
	if len(out.LinkedRecords) == 0 {
		// Entirely legitimate: a definition is complete and usable with no rate
		// attached, because recognition is a use of its own (§7). The worker
		// still gets a credential; there is simply nothing to pay, and they are
		// told that rather than left wondering.
		return 0, "", &HeldReason{
			Code:         "no_rate_attached",
			Explanation:  "this work is recognised but no payment rate is attached to its definition",
			OwnerPartyID: h.payerOwner,
		}
	}

	// A rate is versioned terms (f1_4): several versions can be attached, and
	// the one that prices this unit is the version in force at the release
	// moment — never the latest, which could reprice work already done.
	versions := make([]rateVersion, 0, len(out.LinkedRecords))
	for _, lr := range out.LinkedRecords {
		var p schema.PaymentSetupLinkedRecordPayload
		raw, mErr := json.Marshal(lr.Payload)
		if mErr != nil || json.Unmarshal(raw, &p) != nil {
			return 0, "", &HeldReason{
				Code:         "rate_unreadable",
				Explanation:  "a rate attached to this definition is not in a shape payments understands",
				OwnerPartyID: h.payerOwner,
			}
		}
		versions = append(versions, rateVersion{Version: lr.Version, Payload: p})
	}
	inForce, ok := rateInForceAt(versions, at)
	if !ok {
		return 0, "", &HeldReason{
			Code:         "no_rate_in_force",
			Explanation:  "a rate is published for this work but none of its versions was in force when this payment was released",
			OwnerPartyID: h.payerOwner,
		}
	}
	setup := inForce.Payload

	// Money in minor units, and the multiplication rounded once, explicitly.
	// A float that reaches an amount field is a rounding argument waiting to
	// happen in a reconciliation meeting.
	total := math.Round(unit.Outcome.Value * float64(setup.RatePerOutcomeUnit.AmountMinor))
	if total < 0 || total > math.MaxInt64 {
		return 0, "", &HeldReason{
			Code:         "amount_out_of_range",
			Explanation:  "the amount this record works out to is not a payable number",
			OwnerPartyID: setup.PayerPartyID,
		}
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
		return 0, "", &HeldReason{
			Code:         "nothing_to_pay",
			Explanation:  "this record's outcome is zero, so there is nothing to pay for it",
			OwnerPartyID: setup.PayerPartyID,
		}
	}
	return int64(total), setup.RatePerOutcomeUnit.Currency, nil
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	// Party-filtered: the worker's own money, answered to them or their actor
	// — expanded across any merge first, so the survivor owns the whole
	// history (#100). Unfiltered: a signed-in operations surface (#102).
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	if r.URL.Query().Get("partyId") != "" {
		if _, ok := identity.Authorize(w, r, h.d.Log, ids[0], "",
			h.d.Authenticating, h.d.Permits); !ok {
			return
		}
	} else if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	instructions, err := listInstructions(r.Context(), h.d.DB.Q(), ids, r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list instructions", err)
		return
	}
	if instructions == nil {
		instructions = []Instruction{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"instructions": instructions})
}

func (h *handlers) byClaim(w http.ResponseWriter, r *http.Request) {
	in, err := getInstructionByClaim(r.Context(), h.d.DB.Q(), r.PathValue("claimId"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "instruction", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, in)
}

func (h *handlers) reconciliation(w http.ResponseWriter, r *http.Request) {
	// The money-vs-record gap, with reasons and owners: operations (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	gaps, err := reconcile(r.Context(), h.d.DB.Q())
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
func sendToRail(ctx context.Context, d service.Deps, rail *client.Client, payload json.RawMessage) error {
	var in Instruction
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}

	body := map[string]any{
		"idempotency_key": in.ID,
		"reference":       in.ClaimID,
		"amount_minor":    in.AmountMinor,
		"currency":        in.Currency,
		"destination":     in.PartyID,
	}
	var reply struct {
		Reference string `json:"reference"`
		Status    string `json:"status"`
	}

	comp := Compensation{
		ID:            id.New(d.Clock, "compensation"),
		InstructionID: in.ID,
		UnitID:        in.UnitID,
		AmountMinor:   in.AmountMinor,
		Currency:      in.Currency,
		CreatedAt:     d.Clock.Now(),
	}

	railErr := rail.Post(ctx, "/instructions", body, &reply)
	if railErr != nil {
		// The money may well have moved — a timeout after settlement is the
		// nastiest real failure, and the mock injects it deliberately. So the
		// compensation is recorded as SENT-with-a-failure rather than dropped,
		// and reconciliation is what closes the gap.
		comp.State = "FAILED"
		comp.Failure = &HeldReason{
			Code:         "rail_error",
			Explanation:  "the rail did not confirm this payment: " + railErr.Error(),
			OwnerPartyID: in.PartyID,
		}
	} else {
		at := d.Clock.Now()
		comp.State = "CONFIRMED"
		comp.RailRef = &reply.Reference
		comp.ConfirmedAt = &at
	}

	if err := d.DB.InTx(ctx, func(tx store.Querier) error {
		return upsertCompensation(ctx, tx, comp)
	}); err != nil {
		return err
	}
	// Returning the rail error keeps the message in the outbox for another
	// attempt. The rail is idempotent on the key, so retrying is safe, and a
	// payment that never went out must not be quietly marked delivered.
	return railErr
}
