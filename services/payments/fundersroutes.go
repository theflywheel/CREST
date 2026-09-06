// HTTP surface of the funders wave (F-1, F-2).
//
// Which invariant each endpoint touches:
//
//   - the rate endpoints touch "payments read the version in force at the
//     relevant time" — they can change what an instruction is worth, never
//     whether one exists;
//   - the mechanism endpoints touch f2_9: activation gates DISBURSEMENT only.
//     Nothing here can stop a confirmation-window exit creating its payment
//     obligation (W4); a not-live mechanism turns the obligation into a HELD
//     instruction with a reason and a named owner (W10).
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/services/payments/providers"
)

func fundersRoutes(mux *http.ServeMux, d service.Deps, h *handlers) {
	f := &fundersHandlers{d: d, h: h}

	// F-1: rates as terms.
	mux.HandleFunc("POST /v1/definitions/{id}/rate-owner", f.assignOwner)
	mux.HandleFunc("GET /v1/definitions/{id}/rate-owner", f.getOwner)
	mux.HandleFunc("POST /v1/definitions/{id}/rates", f.publishRate)
	mux.HandleFunc("GET /v1/definitions/{id}/rates", f.listRates)

	// "What funder role does the signed-in caller hold?" — derived from the
	// ownership records, so a console can land a real eSignet login on the
	// rate-owner or mechanism-owner surface without a seeded persona label.
	mux.HandleFunc("GET /v1/rate-ownerships/mine", f.myRateOwnerships)
	mux.HandleFunc("GET /v1/mechanisms/mine", f.myMechanisms)

	// F-2: mechanism to live.
	mux.HandleFunc("POST /v1/mechanisms", f.createMechanism)
	mux.HandleFunc("GET /v1/mechanisms/{id}", f.getMechanism)
	mux.HandleFunc("GET /v1/mechanisms/by-context/{contextId}", f.getMechanismByContext)
	mux.HandleFunc("POST /v1/mechanisms/{id}/test-disbursements", f.testDisburse)
	mux.HandleFunc("POST /v1/mechanisms/{id}/records", f.addRecord)
	mux.HandleFunc("POST /v1/mechanisms/{id}/activate", f.activate)
	mux.HandleFunc("GET /v1/reconciliation/file", f.reconciliationFile)
	mux.HandleFunc("GET /v1/statements", f.statement)
}

type fundersHandlers struct {
	d service.Deps
	h *handlers // the existing payments handlers: definitions client, amountFor
}

// ─── F-1: rate ownership ────────────────────────────────────────────────────

// assignOwner is f1_2: anyone can ask; only this recorded act assigns. A
// re-assignment (f1_5's "hand this to someone else") supersedes and keeps the
// history.
func (f *fundersHandlers) assignOwner(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AssigneePartyID   string `json:"assigneePartyId"`
		AssignedByPartyID string `json:"assignedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if err := validAssignment(body.AssigneePartyID, body.AssignedByPartyID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "%v", err)
		return
	}
	// The assigner acts as themself; what is proved is that the caller is who
	// the record will say assigned it — the dispute posture, not the
	// confirmation one.
	assigner, ok := identity.Authorize(w, r, f.d.Log, body.AssignedByPartyID, "",
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}
	definitionID := r.PathValue("id")
	if !f.definitionExists(w, r.Context(), definitionID) {
		return
	}
	a := RateOwnerAssignment{
		ID:              id.New(f.d.Clock, "rate-owner-assignment"),
		DefinitionID:    definitionID,
		AssigneePartyID: body.AssigneePartyID, AssignedByPartyID: assigner,
		AssignedAt: f.d.Clock.Now(),
	}
	if err := f.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return assignRateOwner(r.Context(), tx, a)
	}); err != nil {
		httpx.Fail(w, f.d.Log, "assign rate owner", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (f *fundersHandlers) getOwner(w http.ResponseWriter, r *http.Request) {
	history, err := rateOwnerHistory(r.Context(), f.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, f.d.Log, "read rate owner", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"current": currentRateOwner(history), "history": history,
	})
}

// myRateOwnerships and myMechanisms answer "which funder role does the caller
// hold?" from the ownership records themselves. The caller is the caller: the
// party the token resolves to, never a party named in a query, so one person
// can only ever read their own ownership. A party that owns neither gets empty
// lists, not an error — "you are not a funder here" is an answer.
func (f *fundersHandlers) myRateOwnerships(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, f.d.Log, f.d.Authenticating) {
		return
	}
	me := identity.From(r.Context()).PartyID
	defs := []string{}
	if me != "" {
		var err error
		if defs, err = rateOwnershipsFor(r.Context(), f.d.DB.Q(), me); err != nil {
			httpx.Fail(w, f.d.Log, "read rate ownerships", err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"partyId": me, "definitionIds": defs})
}

func (f *fundersHandlers) myMechanisms(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, f.d.Log, f.d.Authenticating) {
		return
	}
	me := identity.From(r.Context()).PartyID
	mechs := []Mechanism{}
	if me != "" {
		var err error
		if mechs, err = mechanismsOwnedBy(r.Context(), f.d.DB.Q(), me); err != nil {
			httpx.Fail(w, f.d.Log, "read owned mechanisms", err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"partyId": me, "mechanisms": mechs})
}

// ─── F-1: rates as versioned terms ──────────────────────────────────────────

// publishRate is f1_3 and f1_4 in one act, because the reference's own flow
// is authoring-then-publishing one screen apart and nothing in between: the
// assigned owner prices a unit somebody else defined, and the publication is
// a new payment-setup LinkedRecord version on the definition — the design of
// record's own home for the rate (§10 "linked payment set-up"), named, never
// edited. There is no update route, deliberately and permanently.
func (f *fundersHandlers) publishRate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AuthorPartyID string    `json:"authorPartyId"`
		AmountMinor   int       `json:"amountMinor"`
		Currency      string    `json:"currency"`
		PayerPartyID  string    `json:"payerPartyId"`
		EffectiveFrom time.Time `json:"effectiveFrom"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	author, ok := identity.Authorize(w, r, f.d.Log, body.AuthorPartyID, "",
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}
	definitionID := r.PathValue("id")
	if !f.definitionExists(w, r.Context(), definitionID) {
		return
	}

	history, err := rateOwnerHistory(r.Context(), f.d.DB.Q(), definitionID)
	if err != nil {
		httpx.Fail(w, f.d.Log, "read rate owner", err)
		return
	}
	if err := rateAuthoring(currentRateOwner(history), author); err != nil {
		httpx.WriteError(w, http.StatusForbidden, "not_rate_owner", "%v", err)
		return
	}

	versions, err := f.rateVersions(r.Context(), definitionID)
	if err != nil {
		httpx.Fail(w, f.d.Log, "read existing rates", err)
		return
	}
	if body.EffectiveFrom.IsZero() {
		body.EffectiveFrom = f.d.Clock.Now()
	}
	payload := schema.PaymentSetupLinkedRecordPayload{
		RatePerOutcomeUnit: schema.PaymentSetupLinkedRecordPayloadRatePerOutcomeUnit{
			AmountMinor: body.AmountMinor, Currency: body.Currency,
		},
		PayerPartyID: body.PayerPartyID, EffectiveFrom: body.EffectiveFrom,
		AuthoredByPartyID: &author,
	}
	version := nextRateVersion(versions)
	if version > 1 {
		prev := version - 1
		payload.SupersedesVersion = &prev
	}
	lr := schema.LinkedRecord{
		ID:      id.New(f.d.Clock, "linked-record"),
		Type:    "payment-setup",
		Version: version,
		State:   "ACTIVE",
		KeyedTo: schema.LinkedRecordKeyedTo{
			Kind: schema.LinkedRecordKeyedToKindDefinition, ID: definitionID,
		},
		CreatedAt: f.d.Clock.Now(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		httpx.Fail(w, f.d.Log, "encode rate", err)
		return
	}
	if err := json.Unmarshal(raw, &lr.Payload); err != nil {
		httpx.Fail(w, f.d.Log, "encode rate", err)
		return
	}
	// The definitions service validates the payload against the payment-setup
	// schema and stores it. One store: the rate lives on the definition, and
	// this application holds no second copy that could disagree with it.
	var stored schema.LinkedRecord
	// This is a caller-facing mutation in the definitions registry. Forward
	// the already verified bearer rather than replacing it with a service-only
	// request: definitions must check that the named rate owner is the actual
	// caller. The GET client remains service-to-service because reads are public.
	if err := f.h.postLinkedRecord(r.Context(),
		"/v1/definitions/"+url.PathEscape(definitionID)+"/linked-records", lr,
		r.Header.Get("Authorization"), &stored); err != nil {
		httpx.Fail(w, f.d.Log, "publish rate", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, stored)
}

// postLinkedRecord preserves the human authorization on a payment setup
// publication. pkg/client deliberately signs internal service calls, but this
// endpoint is public and its authorization decision belongs to the caller who
// authored the rate. Requiring a bearer here also prevents a future caller
// from silently turning a missing token into an internal identity.
func (h *handlers) postLinkedRecord(ctx context.Context, path string, in any,
	authorization string, out any) error {
	if strings.TrimSpace(authorization) == "" {
		return errors.New("publishing a rate requires the authenticated caller token")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal linked record: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(h.definitionsURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization)
	resp, err := (&http.Client{
		Timeout: 15 * time.Second,
		// The bearer belongs to the caller and must never be sent to a URL
		// selected by an upstream redirect. Treat every redirect as an
		// upstream response for the caller to resolve explicitly.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("definitions linked record: %s: %s", resp.Status, string(body))
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode linked record: %w", err)
		}
	}
	return nil
}

func (f *fundersHandlers) listRates(w http.ResponseWriter, r *http.Request) {
	versions, err := f.rateVersions(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, f.d.Log, "read rates", err)
		return
	}
	at := f.d.Clock.Now()
	if s := r.URL.Query().Get("at"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "at is not an RFC3339 time")
			return
		}
		at = parsed
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		out = append(out, map[string]any{"version": v.Version, "rate": v.Payload})
	}
	resp := map[string]any{"rates": out, "at": at}
	if inForce, ok := rateInForceAt(versions, at); ok {
		resp["inForce"] = map[string]any{"version": inForce.Version, "rate": inForce.Payload}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// rateVersions reads the definition's payment-setup LinkedRecords and keeps
// the ones whose payload parses. One that does not parse is not silently a
// missing rate — amountFor holds on it — but for authoring, versions are
// counted over everything stored so a new publication never reuses a number.
func (f *fundersHandlers) rateVersions(ctx context.Context, definitionID string) ([]rateVersion, error) {
	var out struct {
		LinkedRecords []schema.LinkedRecord `json:"linkedRecords"`
	}
	err := f.h.definitions.Get(ctx, "/v1/definitions/"+url.PathEscape(definitionID)+
		"/linked-records?type=payment-setup", &out)
	if err != nil {
		return nil, err
	}
	versions := make([]rateVersion, 0, len(out.LinkedRecords))
	for _, lr := range out.LinkedRecords {
		var p schema.PaymentSetupLinkedRecordPayload
		raw, err := json.Marshal(lr.Payload)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		versions = append(versions, rateVersion{ID: lr.ID, Version: lr.Version, Payload: p})
	}
	return versions, nil
}

func (f *fundersHandlers) definitionExists(w http.ResponseWriter, ctx context.Context, definitionID string) bool {
	var def schema.Definition
	if err := f.h.definitions.Get(ctx, "/v1/definitions/"+url.PathEscape(definitionID), &def); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no such definition; a rate prices a unit somebody defined, and this one is not defined")
		return false
	}
	return true
}

// ─── F-2: the mechanism ─────────────────────────────────────────────────────

func (f *fundersHandlers) createMechanism(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContextID        string         `json:"contextId"`
		OwnerPartyID     string         `json:"ownerPartyId"`
		CreatedByPartyID string         `json:"createdByPartyId"`
		Config           map[string]any `json:"config"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.ContextID == "" || body.OwnerPartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a mechanism names its context and its owner; a held payment must have somebody to name (W10)")
		return
	}
	if body.CreatedByPartyID == "" {
		body.CreatedByPartyID = body.OwnerPartyID
	}
	creator, ok := identity.Authorize(w, r, f.d.Log, body.CreatedByPartyID, body.ContextID,
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}
	m := Mechanism{
		ID: id.New(f.d.Clock, "mechanism"), ContextID: body.ContextID,
		OwnerPartyID: body.OwnerPartyID, State: mechanismConfigured,
		Config: body.Config, CreatedByPartyID: creator, CreatedAt: f.d.Clock.Now(),
	}
	created := false
	if err := f.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		created, err = insertMechanism(r.Context(), tx, m)
		return err
	}); err != nil {
		httpx.Fail(w, f.d.Log, "create mechanism", err)
		return
	}
	if !created {
		httpx.WriteError(w, http.StatusConflict, "already_exists",
			"this context already has a payment mechanism; there is one per context, and it is not replaced by asking again")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, f.view(r.Context(), m))
}

// view is a mechanism with its derived standing and readable conditions —
// f1_5 and f2_8 answered on every read, never stored.
func (f *fundersHandlers) view(ctx context.Context, m Mechanism) map[string]any {
	records, err := mechanismRecords(ctx, f.d.DB.Q(), m.ID)
	if err != nil {
		f.d.Log.Error("read mechanism records", "error", err)
	}
	tests, err := testDisbursements(ctx, f.d.DB.Q(), m.ID)
	if err != nil {
		f.d.Log.Error("read test disbursements", "error", err)
	}
	return map[string]any{
		"mechanism":  m,
		"standing":   standing(m),
		"conditions": mechanismConditions(factsFor(records, tests)),
		"records":    records,
		"tests":      tests,
	}
}

func (f *fundersHandlers) getMechanism(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, f.d.Log, f.d.Authenticating) {
		return
	}
	m, err := getMechanism(r.Context(), f.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, f.d.Log, "mechanism", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, f.view(r.Context(), m))
}

func (f *fundersHandlers) getMechanismByContext(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, f.d.Log, f.d.Authenticating) {
		return
	}
	m, err := mechanismByContext(r.Context(), f.d.DB.Q(), r.PathValue("contextId"))
	if errors.Is(err, store.ErrNotFound) {
		// f1_5: half done is a real state, and so is not-started — answered,
		// not errored, so the console can show "payment set up has not begun"
		// rather than a broken screen.
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"mechanism": nil, "standing": "not-configured"})
		return
	}
	if err != nil {
		httpx.Fail(w, f.d.Log, "read mechanism", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, f.view(r.Context(), m))
}

// testDisburse is f2_4: one real payment through the configured mechanism,
// recorded with its result either way. Success is what satisfies the
// test-disbursement-succeeded condition — satisfaction by act, not assertion.
func (f *fundersHandlers) testDisburse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequestedByPartyID string `json:"requestedByPartyId"`
		AmountMinor        int64  `json:"amountMinor"`
		Currency           string `json:"currency"`
		Destination        string `json:"destination"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.AmountMinor <= 0 || body.Currency == "" || body.Destination == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a test disbursement moves a real amount to a real destination: amountMinor, currency and destination are required")
		return
	}
	requester, ok := identity.Authorize(w, r, f.d.Log, body.RequestedByPartyID, "",
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}
	m, err := getMechanism(r.Context(), f.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, f.d.Log, "mechanism", err, store.ErrNotFound)
		return
	}

	t := testDisbursement{
		ID: id.New(f.d.Clock, "test-disbursement"), MechanismID: m.ID,
		RequestedBy: requester, AmountMinor: body.AmountMinor,
		Currency: body.Currency, Destination: body.Destination,
		At: f.d.Clock.Now(),
	}
	reply, providerErr := f.h.rail.Submit(r.Context(), providers.Request{
		IdempotencyKey: t.ID, InstructionID: t.ID, ContextID: m.ContextID, Reference: "test:" + t.ID,
		AmountMinor: t.AmountMinor, Currency: t.Currency, Destination: t.Destination,
	})
	settledAmount, settledCurrency, settled := providers.SettledAmount(reply)
	if providerErr != nil || normalizeRailStatus(reply.Status) != providers.Confirmed || reply.Reference == "" || !settled || settledAmount != t.AmountMinor || settledCurrency != t.Currency {
		t.State = "FAILED"
		t.Failure = &HeldReason{
			Code:         "provider_not_settled",
			Explanation:  "the configured provider did not prove terminal settlement for the test disbursement",
			OwnerPartyID: m.OwnerPartyID,
		}
	} else {
		t.State = "SUCCEEDED"
		t.RailRef = &reply.Reference
	}
	if err := f.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertTestDisbursement(r.Context(), tx, t)
	}); err != nil {
		httpx.Fail(w, f.d.Log, "record test disbursement", err)
		return
	}
	// A failed test is still a 201: the test happened and its result is the
	// record (f2_4). What it is not is a satisfied condition.
	httpx.WriteJSON(w, http.StatusCreated, t)
}

// addRecord files one recorded act of setting the mechanism up:
// reconciliation-agreement (f2_5), statement-agreement (f2_6),
// batching-choice (f2_7), qualification-submitted and
// qualification-verified (f2_9, f2_10). The payload values are L2 and stored
// opaquely; the actor and the moment are the record.
func (f *fundersHandlers) addRecord(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind         string         `json:"kind"`
		ActorPartyID string         `json:"actorPartyId"`
		Payload      map[string]any `json:"payload"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	switch body.Kind {
	case recordRailsChosen, recordProviderConnected,
		recordReconciliationAgreement, recordStatementAgreement,
		recordBatchingChoice, recordQualificationSubmitted, recordQualificationVerified:
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"kind is one of the recorded set-up acts: rails-chosen, provider-connected, reconciliation-agreement, statement-agreement, batching-choice, qualification-submitted, qualification-verified")
		return
	}
	actor, ok := identity.Authorize(w, r, f.d.Log, body.ActorPartyID, "",
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}
	m, err := getMechanism(r.Context(), f.d.DB.Q(), r.PathValue("id"), false)
	if err != nil {
		httpx.NotFoundOr(w, f.d.Log, "mechanism", err, store.ErrNotFound)
		return
	}

	if body.Kind == recordBatchingChoice {
		window, _ := body.Payload["window"].(string)
		tradeoff, _ := body.Payload["tradeoff"].(string)
		if err := validBatchingChoice(window, tradeoff); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "%v", err)
			return
		}
	}
	if body.Kind == recordQualificationVerified {
		records, err := mechanismRecords(r.Context(), f.d.DB.Q(), m.ID)
		if err != nil {
			httpx.Fail(w, f.d.Log, "read mechanism records", err)
			return
		}
		if factsFor(records, nil).QualificationSubmitted == nil {
			httpx.WriteError(w, http.StatusConflict, "nothing_submitted",
				"nothing has been submitted for verification; a verification of nothing verifies nothing (f2_9)")
			return
		}
	}

	rec := mechanismRecord{
		ID: id.New(f.d.Clock, "mechanism-record"), MechanismID: m.ID,
		Kind: body.Kind, ActorPartyID: actor, Payload: body.Payload,
		At: f.d.Clock.Now(),
	}
	if err := f.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insertMechanismRecord(r.Context(), tx, rec)
	}); err != nil {
		httpx.Fail(w, f.d.Log, "record mechanism act", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rec)
}

// activateMechanismAndRelease is the activation transaction used by the HTTP
// handler. Keeping the read, gate decision, state transition and held sweep in
// one transaction is important: a concurrent activation cannot observe the
// same mechanism hold and enqueue it twice, and the sweep never asks the rate
// or evidence dependencies to re-price an existing obligation.
func activateMechanismAndRelease(ctx context.Context, tx store.Querier, mechanismID, activator string, now time.Time) (Mechanism, []activationCondition, []string, error) {
	var out Mechanism
	var conds []activationCondition
	released := []string{}
	err := func() error {
		m, err := getMechanism(ctx, tx, mechanismID, true)
		if err != nil {
			return err
		}
		records, err := mechanismRecords(ctx, tx, m.ID)
		if err != nil {
			return err
		}
		tests, err := testDisbursements(ctx, tx, m.ID)
		if err != nil {
			return err
		}
		wasActive := m.State == mechanismActive
		out, conds, err = activateMechanism(m, factsFor(records, tests), activator, now)
		if err != nil {
			return err
		}
		if wasActive {
			return nil // idempotent; the held sweep already ran when it went live
		}
		if err := markMechanismActive(ctx, tx, out); err != nil {
			return err
		}
		// The payments this gate was holding. They were priced when the
		// obligation was created; activation only clears the sending gate and
		// preserves the immutable amount and rate audit fields. Never back to
		// silence.
		held, err := heldForMechanism(ctx, tx, out.ContextID)
		if err != nil {
			return err
		}
		for _, in := range held {
			// Activation only clears the mechanism gate. The instruction was
			// already priced when the obligation was created, so preserve its
			// amount, rate link, version and pricing instant exactly. Calling
			// amountFor here would silently reprice an old obligation after a
			// later rate publication.
			in = releaseMechanismHeld(in)
			if err := releaseHeldInstructionAndEnqueue(ctx, tx, in); err != nil {
				return err
			}
			released = append(released, in.ID)
		}
		return nil
	}()
	return out, conds, released, err
}

// activate is f2_8 and f2_10: the last gate, and what it opened. On success
// the mechanism goes ACTIVE and every instruction this mechanism was holding
// under mechanism_not_live is released and sent — activation is what lets
// disbursement flow, and the obligations were already there waiting (f2_9).
func (f *fundersHandlers) activate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ActivatedByPartyID string `json:"activatedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	activator, ok := identity.Authorize(w, r, f.d.Log, body.ActivatedByPartyID, "",
		f.d.Authenticating, f.d.Permits)
	if !ok {
		return
	}

	var out Mechanism
	var conds []activationCondition
	released := []string{}
	err := f.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		out, conds, released, err = activateMechanismAndRelease(r.Context(), tx, r.PathValue("id"), activator, f.d.Clock.Now())
		return err
	})
	switch {
	case errors.Is(err, errMechanismGatesUnmet):
		// Refused with the conditions readable — never a bare no (#173's
		// posture).
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "gates_unmet", "message": errMechanismGatesUnmet.Error(),
			"conditions": conds,
		})
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such mechanism")
	case err != nil:
		httpx.Fail(w, f.d.Log, "activate mechanism", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"mechanism": out, "standing": standing(out), "conditions": conds,
			"releasedInstructionIds": released,
		})
	}
}

// ─── F-2: the reconciliation file and the statement ─────────────────────────

// reconciliationFileColumns is the export contract (f2_5), version
// crest-recon-csv-v1: CSV, one line per payment instruction, every line tied
// back by instruction id, claim id and rail reference. Append-only as a
// contract: a column may be added, never renamed or removed.
var reconciliationFileColumns = []string{
	"instruction_id", "claim_id", "unit_id", "payee_party_id", "state",
	"amount_minor", "currency", "released_by", "released_at",
	"rail_state", "rail_ref", "held_code", "held_owner",
}

func (f *fundersHandlers) reconciliationFile(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, f.d.Log, f.d.Authenticating) {
		return
	}
	lines, err := reconciliationLines(r.Context(), f.d.DB.Q())
	if err != nil {
		httpx.Fail(w, f.d.Log, "read reconciliation lines", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("X-CREST-Reconciliation-Format", "crest-recon-csv-v1")
	cw := csv.NewWriter(w)
	_ = cw.Write(reconciliationFileColumns)
	for _, l := range lines {
		_ = cw.Write([]string{
			l.InstructionID, l.ClaimID, l.UnitID, l.PartyID, l.State,
			strconv.FormatInt(l.AmountMinor, 10), l.Currency, l.ReleasedBy,
			l.ReleasedAt.UTC().Format(time.RFC3339), l.RailState, l.RailRef,
			l.HeldCode, l.HeldOwner,
		})
	}
	cw.Flush()
}

// statement is f2_6: an advisory monthly statement with its limits stated on
// it, every time, rather than assumed known.
func (f *fundersHandlers) statement(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("partyId") == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"partyId is required: a statement is one party's month, not a report")
		return
	}
	month := r.URL.Query().Get("month")
	from, err := time.Parse("2006-01", month)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_query", "month is YYYY-MM")
		return
	}
	to := from.AddDate(0, 1, 0)
	ids, ok := sameParty(w, r, f.d)
	if !ok {
		return
	}
	if _, ok := identity.Authorize(w, r, f.d.Log, ids[0], "",
		f.d.Authenticating, f.d.Permits); !ok {
		return
	}
	instructions, err := statementInstructions(r.Context(), f.d.DB.Q(), ids, from, to)
	if err != nil {
		httpx.Fail(w, f.d.Log, "read statement", err)
		return
	}
	if instructions == nil {
		instructions = []Instruction{}
	}
	totals := map[string]int64{}
	heldCount := 0
	for _, in := range instructions {
		if in.State == "HELD" {
			heldCount++
			continue
		}
		totals[in.Currency] += in.AmountMinor
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"partyId": ids[0], "month": month,
		"instructions":          instructions,
		"totalsMinorByCurrency": totals, "heldCount": heldCount,
		"generatedAt": f.d.Clock.Now(),
		"limits":      statementLimits(),
	})
}
