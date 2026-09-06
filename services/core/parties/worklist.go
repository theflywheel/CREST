package parties

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// g4_5, "A worklist, not a completeness chart" (§4).
//
// No new quality score is invented here. Every gap named below is a fact the
// registry already keeps a typed record for: an identity binding is present or
// it is not (§4.1), an enrolment consent is granted or it is not (§9), a party
// id sits in an open match hold or it does not (§4). The worklist is those
// three facts joined per party, nothing scored or weighted.
const (
	gapNoIdentityBinding  = "no_identity_binding"
	gapNoEnrolmentConsent = "no_enrolment_consent"
	gapUnresolvedHold     = "unresolved_hold"
)

// worklistGap is one named, fixable thing missing from a party's record.
type worklistGap struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	FixableBy string `json:"fixableBy"`
}

// worklistRow is a party with at least one gap. A party with none never
// appears — the point of a worklist over a completeness chart is that it is
// exactly as long as the work remaining.
type worklistRow struct {
	PartyID     string        `json:"partyId"`
	DisplayName string        `json:"displayName"`
	Gaps        []worklistGap `json:"gaps"`
}

// partyGapInputs is what qualityGaps needs about one party, gathered from
// three already-existing stores (identity bindings live on the party
// document; consent and hold state are separate reads). Keeping this as a
// plain struct is what lets qualityGaps be tested without a database.
type partyGapInputs struct {
	HasIdentityBinding  bool
	HasEnrolmentConsent bool
	HasOpenHold         bool
}

// qualityGaps is the pure judgement: which named gaps does this party have.
// It never scores or ranks — a party either has a gap or it does not, and the
// worklist shows the fact plainly rather than folding it into a number.
func qualityGaps(in partyGapInputs) []worklistGap {
	var gaps []worklistGap
	if !in.HasIdentityBinding {
		gaps = append(gaps, worklistGap{
			Kind:      gapNoIdentityBinding,
			Detail:    "no identity binding has ever been recorded for this party",
			FixableBy: "the worker themselves, or an enrolling agent assisting them",
		})
	}
	if !in.HasEnrolmentConsent {
		gaps = append(gaps, worklistGap{
			Kind:      gapNoEnrolmentConsent,
			Detail:    "no enrolment consent is on file, so evidence cannot yet be held about this worker",
			FixableBy: "the enrolling agent or project that registered this party",
		})
	}
	if in.HasOpenHold {
		gaps = append(gaps, worklistGap{
			Kind:      gapUnresolvedHold,
			Detail:    "this party's identifiers collide with another party's in an open match hold",
			FixableBy: "a registry custodian, deciding the hold",
		})
	}
	return gaps
}

// buildWorklist joins party facts with pre-computed gap inputs into rows,
// dropping any party with no gap. Kept separate from qualityGaps so the
// dropping rule — silence for a clean party — is itself one visible line.
func buildWorklist(parties []schema.Party, inputs map[string]partyGapInputs) []worklistRow {
	rows := make([]worklistRow, 0, len(parties))
	for _, p := range parties {
		gaps := qualityGaps(inputs[p.ID])
		if len(gaps) == 0 {
			continue
		}
		rows = append(rows, worklistRow{PartyID: p.ID, DisplayName: p.DisplayName, Gaps: gaps})
	}
	return rows
}

// partiesPage reads a page of non-merged parties of a kind, oldest first, for
// the worklist to walk. Pagination exists because a registry custodian's
// queue over a real deployment's roster cannot be one unbounded read.
func partiesPage(ctx context.Context, q store.Querier, kind string, limit, offset int) ([]schema.Party, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM parties
		WHERE kind = $1 AND (doc->>'mergedInto') IS NULL
		ORDER BY created_at, id
		LIMIT $2 OFFSET $3`, kind, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Party, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.Party{}, err
		}
		var p schema.Party
		return p, json.Unmarshal(doc, &p)
	})
}

// enrolmentConsentExists answers "has this party EVER had a live enrolment
// consent, in any context" — narrower than enrolmentConsentOf, which is
// scoped to one context because permits() needs a per-context answer. The
// worklist asks the party-wide question: is there any project this worker
// could have evidence held for at all.
func enrolmentConsentExists(ctx context.Context, q store.Querier, partyIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(partyIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT party_id FROM consents
		WHERE party_id = ANY($1) AND moment = 'enrolment' AND revoked_at IS NULL`, partyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids, err := store.Collect(rows, func(r store.Row) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// partiesWithOpenHolds is which of a set of party ids appear as a candidate in
// any open match hold (§4). Membership only — never the colliding key value,
// per Hold's own doc comment on why keyValue is unserialised.
func partiesWithOpenHolds(ctx context.Context, q store.Querier, partyIDs []string) (map[string]bool, error) {
	holds, err := openHolds(ctx, q)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range partyIDs {
		want[id] = true
	}
	out := map[string]bool{}
	for _, h := range holds {
		for _, c := range h.Candidates {
			if want[c] {
				out[c] = true
			}
		}
	}
	return out, nil
}

const defaultWorklistPageSize = 200

// qualityWorklist is g4_5's endpoint: GET /v1/quality-worklist?kind=&limit=&offset=
func (h *handlers) qualityWorklist(w http.ResponseWriter, r *http.Request) {
	// This queue includes worker names and identity gaps. It is assigned to the
	// registry custodian, not a general authenticated caller.
	if _, ok := requireRegistryCustodian(w, r, h.d, ""); !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind == "" {
		kind = "person"
	}
	limit := defaultWorklistPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_parameter", "limit must be a positive integer")
			return
		}
		limit = n
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_parameter", "offset must be a non-negative integer")
			return
		}
		offset = n
	}

	parties, err := partiesPage(r.Context(), h.d.DB.Q(), kind, limit, offset)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read parties page", err)
		return
	}
	ids := make([]string, len(parties))
	for i, p := range parties {
		ids[i] = p.ID
	}
	hasConsent, err := enrolmentConsentExists(r.Context(), h.d.DB.Q(), ids)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read enrolment consents", err)
		return
	}
	hasOpenHold, err := partiesWithOpenHolds(r.Context(), h.d.DB.Q(), ids)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read open holds", err)
		return
	}

	inputs := make(map[string]partyGapInputs, len(parties))
	for _, p := range parties {
		inputs[p.ID] = partyGapInputs{
			HasIdentityBinding:  len(p.IdentityBindings) > 0,
			HasEnrolmentConsent: hasConsent[p.ID],
			HasOpenHold:         hasOpenHold[p.ID],
		}
	}
	rows := buildWorklist(parties, inputs)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"rows":     rows,
		"scanned":  len(parties),
		"withGaps": len(rows),
		"limit":    limit,
		"offset":   offset,
	})
}
