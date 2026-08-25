package main

// Recovery (§16, #106): how a worker who lost their device gets their record
// back, and the one exception to it that can never be quiet.
//
// The dead-end this closes: identity binds to a device-held subject, so a lost
// handset would strand the person their record exists for. Two ways out:
//
//   - 2-of-3. Two people confirm this is the same person. Each confirmer must
//     be appointed by a DIFFERENT authority — a panel appointed by one
//     organisation is that organisation's decision wearing several names — and
//     that rule lives in a UNIQUE constraint, not here. "Of three" is not a
//     pre-named panel: any two eligible voices suffice, because naming three
//     people in advance is hardest exactly where recovery is needed most.
//   - Supervisor override, for when two confirmers cannot be reached. Refused
//     unless it carries a reason, an owner and a review-by date — by this
//     handler AND by a database constraint, the way an unconfirmed merge is
//     impossible to express rather than discouraged.
//
// What a confirmer confirms is the narrow question: that the person asking is
// the person the record is about. Not that they should have access, not that
// they are in good standing — those are authorization questions with their own
// owners.
//
// Recovery ends in an appended identity binding of class "recovery", never in
// an edit: the old bindings stay, because they are what a later dispute about
// this recovery is answered from. Assurance derives from the binding class
// (assurance.go): a recovered worker sits at IA-1 until they re-anchor with a
// real provider, at which point the stronger binding simply wins — vouching is
// community knowledge, not a national identity check, and the tier must not
// say otherwise.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// FunctionOverrideRecovery is its own function, deliberately not act-for-party:
// acting for a worker day to day and deciding alone that somebody is who they
// claim to be are different powers, and a deployment must be able to grant one
// without the other.
//
// Operator-only, per G1 (#10), which settled this before #105 re-asked it and
// stands: the person who caused a lockout — by coercion, or by being the
// guardian who will not cooperate — is frequently the worker's own supervisor,
// and a recovery path they can invoke is a path they can invoke AGAINST the
// worker. Two teeth, both structural: the permits check below matches only an
// instance-scoped grant (an operator-level power, never a context one), and
// the handler refuses the overrider named as the worker's own supervisor
// outright, whatever they hold.
const FunctionOverrideRecovery = "override-recovery"

// confirmationsNeeded is the "2" of 2-of-3, and it is L1: two deployments that
// disagreed about how many voices re-establish an identity would not both be
// CREST.
const confirmationsNeeded = 2

func registerRecoveryRoutes(mux *http.ServeMux, d service.Deps) {
	// How long an override stands before it must be looked at. L2 — a pilot
	// and a national programme can reasonably differ — with an L1 default.
	reviewAfter, err := config.Duration("CREST_RECOVERY_OVERRIDE_REVIEW", 90*24*time.Hour)
	if err != nil {
		d.Log.Error("CREST_RECOVERY_OVERRIDE_REVIEW is unreadable", "error", err)
		reviewAfter = 90 * 24 * time.Hour
	}
	h := &recoveryHandlers{d: d, reviewAfter: reviewAfter}
	mux.HandleFunc("POST /v1/recoveries", h.open)
	mux.HandleFunc("GET /v1/recoveries/{id}", h.get)
	mux.HandleFunc("GET /v1/recoveries", h.list)
	mux.HandleFunc("POST /v1/recoveries/{id}/confirmations", h.confirm)
	mux.HandleFunc("POST /v1/recoveries/{id}/override", h.override)
	mux.HandleFunc("POST /v1/recoveries/{id}/complete", h.complete)
}

type recoveryHandlers struct {
	d           service.Deps
	reviewAfter time.Duration
}

// Recovery is the full record, override included. Readable afterwards by the
// worker and by an auditor — an override nobody can read back is quiet, which
// is the one thing it must never be.
type Recovery struct {
	ID       string    `json:"id"`
	PartyID  string    `json:"partyId"`
	OpenedBy string    `json:"openedByPartyId"`
	Reason   string    `json:"reason"`
	State    string    `json:"state"`
	Created  time.Time `json:"createdAt"`

	Confirmations []RecoveryConfirmation `json:"confirmations"`

	OverrideBy     *string    `json:"overrideByPartyId,omitempty"`
	OverrideReason *string    `json:"overrideReason,omitempty"`
	OverrideAt     *time.Time `json:"overrideAt,omitempty"`
	ReviewBy       *time.Time `json:"reviewBy,omitempty"`

	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type RecoveryConfirmation struct {
	ConfirmerPartyID string    `json:"confirmerPartyId"`
	AuthorityPartyID string    `json:"authorityPartyId"`
	ConfirmedAt      time.Time `json:"confirmedAt"`
}

func (h *recoveryHandlers) open(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PartyID  string `json:"partyId"`
		OpenedBy string `json:"openedByPartyId"`
		Reason   string `json:"reason"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.PartyID == "" || body.OpenedBy == "" || body.Reason == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"partyId, openedByPartyId and reason are all required: a recovery opened by "+
				"nobody for no reason is not a record anybody can audit")
		return
	}
	// The opener is the caller (#102). The worker themselves cannot be — the
	// premise of a recovery is that they cannot authenticate — which is why
	// the opener is named and checked instead.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.OpenedBy, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	rec := Recovery{
		ID: id.New(h.d.Clock, "recovery"), PartyID: body.PartyID,
		OpenedBy: body.OpenedBy, Reason: body.Reason, State: "OPEN",
		Created: h.d.Clock.Now(), Confirmations: []RecoveryConfirmation{},
	}
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		if _, err := getParty(r.Context(), tx, body.PartyID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO recoveries (id, party_id, opened_by, reason, state, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			rec.ID, rec.PartyID, rec.OpenedBy, rec.Reason, rec.State, rec.Created)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such party")
		return
	case store.IsUniqueViolation(err):
		// The partial unique index: one live recovery per person. Two open
		// panels could answer the same question differently.
		httpx.WriteError(w, http.StatusConflict, "recovery_already_open",
			"a recovery for this party is already in progress; confirm or complete that one")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "open recovery", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, rec)
}

func (h *recoveryHandlers) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConfirmerPartyID string `json:"confirmerPartyId"`
		// Declared and then verified, rather than inferred: a confirmer who
		// holds authorizations from two organisations must say which hat they
		// are wearing, because the anti-stacking rule counts authorities.
		AuthorityPartyID string `json:"authorityPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.ConfirmerPartyID == "" || body.AuthorityPartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"confirmerPartyId and authorityPartyId are both required")
		return
	}
	// A voice must belong to the person speaking (#102): the confirmer is the
	// caller. Without this, one person could be all three voices.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.ConfirmerPartyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	now := h.d.Clock.Now()
	var rec Recovery
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := getRecovery(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		rec = got
		if rec.State != "OPEN" {
			return errRecoveryNotOpen
		}
		if body.ConfirmerPartyID == rec.PartyID {
			return errSelfConfirmation
		}
		// The declared authority must actually stand behind the confirmer: an
		// ACTIVE authorization from that authority, current now. Which
		// function it grants does not matter — what is being verified is the
		// relationship, not a permission.
		vouched, err := authorityStandsBehind(r.Context(), tx,
			body.AuthorityPartyID, body.ConfirmerPartyID, now)
		if err != nil {
			return err
		}
		if !vouched {
			return errNotVouchedFor
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO recovery_confirmations (recovery_id, confirmer_party_id, authority_party_id, confirmed_at)
			VALUES ($1, $2, $3, $4)`,
			rec.ID, body.ConfirmerPartyID, body.AuthorityPartyID, now); err != nil {
			return err
		}
		var distinct int
		if err := tx.QueryRow(r.Context(),
			`SELECT count(DISTINCT authority_party_id) FROM recovery_confirmations WHERE recovery_id = $1`,
			rec.ID).Scan(&distinct); err != nil {
			return err
		}
		if distinct >= confirmationsNeeded {
			if _, err := tx.Exec(r.Context(),
				`UPDATE recoveries SET state = 'CONFIRMED' WHERE id = $1`, rec.ID); err != nil {
				return err
			}
		}
		rec, err = getRecovery(r.Context(), tx, rec.ID)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such recovery")
		return
	case errors.Is(err, errRecoveryNotOpen):
		httpx.WriteError(w, http.StatusConflict, "recovery_not_open",
			"this recovery is not collecting confirmations any more")
		return
	case errors.Is(err, errSelfConfirmation):
		httpx.WriteError(w, http.StatusBadRequest, "self_confirmation",
			"a person cannot confirm their own recovery")
		return
	case errors.Is(err, errNotVouchedFor):
		httpx.WriteError(w, http.StatusForbidden, "not_vouched_for",
			"the declared authority holds no active authorization for this confirmer, "+
				"so it does not stand behind them")
		return
	case store.IsUniqueViolation(err):
		// Either the same confirmer twice, or — the one that matters — a second
		// confirmer appointed by the same organisation. One org, one voice.
		httpx.WriteError(w, http.StatusConflict, "authority_already_counted",
			"this confirmer, or another confirmer from the same authority, has already "+
				"confirmed; a second voice must come from a different authority")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "confirm recovery", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *recoveryHandlers) override(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ByPartyID string `json:"byPartyId"`
		Reason    string `json:"reason"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	// The refusal this endpoint exists for. An override with no reason or no
	// owner must be impossible to express — here, and again in the table's
	// CHECK constraint for any path that gets past this handler.
	if body.ByPartyID == "" || body.Reason == "" {
		httpx.WriteError(w, http.StatusBadRequest, "override_needs_a_reason_and_an_owner",
			"byPartyId and reason are both required: the safeguard on an override is not "+
				"that it is hard to obtain but that it can never be quiet")
		return
	}
	// The overrider is the caller (#102) — before their function is even
	// checked, they must be who the body says.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.ByPartyID, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	now := h.d.Clock.Now()
	permitted, _, err := permits(r.Context(), h.d.DB.Q(),
		body.ByPartyID, FunctionOverrideRecovery, "", now)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check override authorization", err)
		return
	}
	if !permitted {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"no active authorization grants %s the %s function", body.ByPartyID, FunctionOverrideRecovery)
		return
	}
	var rec Recovery
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := getRecovery(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if got.State != "OPEN" {
			return errRecoveryNotOpen
		}
		// G1's line, held whatever the overrider was granted: the worker's own
		// supervisor never overrides their recovery. The supervisor is read
		// off the worker's record, not off the request.
		worker, err := getParty(r.Context(), tx, got.PartyID)
		if err != nil {
			return err
		}
		for _, route := range worker.ContactRoutes {
			if route.Kind == schema.PartyContactRoutesItemKindSupervisor &&
				route.Value == body.ByPartyID {
				return errOwnSupervisor
			}
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE recoveries SET state = 'OVERRIDDEN',
			  override_by = $2, override_reason = $3, override_at = $4, review_by = $5
			WHERE id = $1`,
			got.ID, body.ByPartyID, body.Reason, now, now.Add(h.reviewAfter)); err != nil {
			return err
		}
		rec, err = getRecovery(r.Context(), tx, got.ID)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such recovery")
		return
	case errors.Is(err, errRecoveryNotOpen):
		httpx.WriteError(w, http.StatusConflict, "recovery_not_open",
			"this recovery is past the point where an override applies")
		return
	case errors.Is(err, errOwnSupervisor):
		httpx.WriteError(w, http.StatusForbidden, "own_supervisor_cannot_override",
			"a worker's own supervisor never overrides their recovery (G1, #10): the person "+
				"who caused a lockout is frequently the supervisor, and a path they can invoke "+
				"is a path they can invoke against the worker")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "override recovery", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *recoveryHandlers) complete(w http.ResponseWriter, r *http.Request) {
	// Completion needs a signed-in caller — the agent binding the new subject
	// — but not a named identity check: the authority for the completion is
	// the decided recovery itself, and the binding it appends is inert until
	// the worker actually authenticates as that subject (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	var body struct {
		// The new subject the worker will authenticate as from now on —
		// whatever route they now hold. Appended, never substituted: the old
		// bindings stay (§4.1).
		SubjectRef string `json:"subjectRef"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.SubjectRef == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"subjectRef is required: a recovery that binds nothing leaves the worker recovered and still unable to authenticate")
		return
	}
	now := h.d.Clock.Now()
	var rec Recovery
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		got, err := getRecovery(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if got.State != "CONFIRMED" && got.State != "OVERRIDDEN" {
			return errRecoveryNotDecided
		}
		// provider names which path decided it, so the binding itself says how
		// this identity was re-established — an input assurance and any later
		// dispute can read without joining back to this table.
		provider := "recovery:2-of-" + itoa(confirmationsNeeded+1)
		if got.State == "OVERRIDDEN" {
			provider = "recovery:override"
		}
		if _, _, err := appendBinding(r.Context(), tx, got.PartyID, schema.PartyIdentityBindingsItem{
			Provider:      provider,
			ProviderClass: schema.PartyIdentityBindingsItemProviderClassRecovery,
			SubjectRef:    body.SubjectRef,
			AssertedAt:    now,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE recoveries SET state = 'COMPLETED', completed_at = $2 WHERE id = $1`,
			got.ID, now); err != nil {
			return err
		}
		rec, err = getRecovery(r.Context(), tx, got.ID)
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such recovery")
		return
	case errors.Is(err, errRecoveryNotDecided):
		httpx.WriteError(w, http.StatusConflict, "recovery_not_decided",
			"a recovery completes only after two confirmations from distinct authorities, or an override")
		return
	case errors.Is(err, ErrBindingBelongsToAnother):
		httpx.WriteError(w, http.StatusConflict, "subject_already_bound",
			"that subject already authenticates another party")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "complete recovery", err)
		return
	}
	if h.d.ForgetSubject != nil {
		h.d.ForgetSubject(body.SubjectRef)
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

func (h *recoveryHandlers) get(w http.ResponseWriter, r *http.Request) {
	rec, err := getRecovery(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "recovery", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rec)
}

// list is the audit surface. ?overdue=true narrows it to overridden
// recoveries whose review-by date has passed — the queue "flagged for review,
// never silent" promises somebody is reading.
func (h *recoveryHandlers) list(w http.ResponseWriter, r *http.Request) {
	// The audit surface: signed-in callers (#102). Reading ONE recovery stays
	// open below — the id is unguessable and the worker it belongs to may
	// hold it printed on paper, which is exactly who must never be locked out
	// of reading it.
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	where, args := ``, []any{}
	if r.URL.Query().Get("overdue") == "true" {
		where = `WHERE review_by IS NOT NULL AND review_by < $1`
		args = append(args, h.d.Clock.Now())
	}
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT id FROM recoveries `+where+` ORDER BY created_at, id`, args...)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list recoveries", err)
		return
	}
	ids, err := store.Collect(rows, func(r store.Row) (string, error) {
		var v string
		return v, r.Scan(&v)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "list recoveries", err)
		return
	}
	out := []Recovery{}
	for _, rid := range ids {
		rec, err := getRecovery(r.Context(), h.d.DB.Q(), rid)
		if err != nil {
			httpx.Fail(w, h.d.Log, "read recovery", err)
			return
		}
		out = append(out, rec)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"recoveries": out, "count": len(out)})
}

var (
	errRecoveryNotOpen    = errors.New("recovery is not open")
	errRecoveryNotDecided = errors.New("recovery is not decided")
	errSelfConfirmation   = errors.New("self confirmation")
	errOwnSupervisor      = errors.New("own supervisor cannot override")
	errNotVouchedFor      = errors.New("not vouched for")
)

func getRecovery(ctx context.Context, q store.Querier, id string) (Recovery, error) {
	var rec Recovery
	err := q.QueryRow(ctx, `
		SELECT id, party_id, opened_by, reason, state, created_at,
		       override_by, override_reason, override_at, review_by, completed_at
		FROM recoveries WHERE id = $1`, id).Scan(
		&rec.ID, &rec.PartyID, &rec.OpenedBy, &rec.Reason, &rec.State, &rec.Created,
		&rec.OverrideBy, &rec.OverrideReason, &rec.OverrideAt, &rec.ReviewBy, &rec.CompletedAt)
	if err != nil {
		return Recovery{}, err
	}
	rows, err := q.Query(ctx, `
		SELECT confirmer_party_id, authority_party_id, confirmed_at
		FROM recovery_confirmations WHERE recovery_id = $1 ORDER BY confirmed_at`, rec.ID)
	if err != nil {
		return Recovery{}, err
	}
	rec.Confirmations, err = store.Collect(rows, func(r store.Row) (RecoveryConfirmation, error) {
		var c RecoveryConfirmation
		return c, r.Scan(&c.ConfirmerPartyID, &c.AuthorityPartyID, &c.ConfirmedAt)
	})
	if rec.Confirmations == nil {
		rec.Confirmations = []RecoveryConfirmation{}
	}
	return rec, err
}

// authorityStandsBehind reports whether the authority currently holds an
// ACTIVE authorization naming this confirmer — any function, any scope.
func authorityStandsBehind(ctx context.Context, q store.Querier,
	authorityID, confirmerID string, at time.Time) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM authorizations
		WHERE party_id = $1 AND state = 'ACTIVE'
		  AND doc ->> 'authorityPartyId' = $2
		  AND period_start <= $3
		  AND (period_end IS NULL OR period_end >= $3)`,
		confirmerID, authorityID, at).Scan(&n)
	return n > 0, err
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
