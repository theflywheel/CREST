package evidence

import (
	"context"
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/store"
)

// g4_7, "The number that says whether this is infrastructure or an app" (§2,
// §3).
//
// The unit/claim split is the load-bearing shape §2 names: a Unit carries
// which context submitted it (schema.Unit.ContextID); a Claim links a Party to
// one. Joining the two gives an honest answer to "how many distinct contexts
// have claimed against the same worker" without inventing any new record —
// it is the one number the existing tables can answer truthfully, and it is
// stated here rather than assumed: a party claimed by two contexts is read as
// reuse regardless of what state either claim is in, because a disputed claim
// still names who submitted work about this worker (a dispute contests the
// record, W5 — it does not erase that the submission happened).
//
// This is deliberately not "distinct organisations": a context is a project
// or campaign (schema.Context.Kind, deployment vocabulary), and the same
// organisation running two contexts against the same worker is two reuses of
// the registry, not one — reuse is about how many independent efforts found
// this worker's record rather than re-registering them, and independence is
// tracked at the context, which is the unit of "who is submitting" the schema
// actually carries.

// partyContextSpread is one party's count of distinct submitting contexts,
// gathered from claims joined to units. Kept as a plain slice input so
// reuseMetric is testable without a database.
type partyContextSpread struct {
	PartyID  string
	Contexts int
}

// reuseMetric is the pure computation: given how many distinct contexts have
// claimed against each party, and how many contexts exist across all claims,
// what fraction of claimed workers were found by more than one.
//
// An empty registry (no claims at all) reports zero of zero rather than
// dividing — "no data yet" and "no reuse" are different facts, and the caller
// gets rate=nil to tell them apart.
func reuseMetric(spread []partyContextSpread, distinctContexts int) map[string]any {
	total := len(spread)
	reused := 0
	for _, s := range spread {
		if s.Contexts > 1 {
			reused++
		}
	}
	out := map[string]any{
		"totalClaimedParties": total,
		"reusedParties":       reused,
		"distinctContexts":    distinctContexts,
		"derivation": "reusedParties counts workers whose claims (any state) span more than " +
			"one distinct submitting context, joining claims to units on unit_id and " +
			"reading unit.contextId; reuseRate = reusedParties / totalClaimedParties. " +
			"A disputed claim still counts: a dispute contests the record, it does not " +
			"erase that the submission happened.",
	}
	if total > 0 {
		out["reuseRate"] = float64(reused) / float64(total)
	} else {
		out["reuseRate"] = nil
	}
	return out
}

// contextSpreadPerParty reads, for every party with at least one claim, how
// many distinct contexts (via the claim's unit) have claimed against them —
// and the total number of distinct contexts across all claims.
func contextSpreadPerParty(ctx context.Context, q store.Querier, contextID string) ([]partyContextSpread, int, error) {
	rows, err := q.Query(ctx, `
		SELECT c.party_id, count(DISTINCT u.context_id)
		FROM claims c JOIN units u ON u.id = c.unit_id
		WHERE u.context_id = $1
		GROUP BY c.party_id`, contextID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	spread, err := store.Collect(rows, func(r store.Row) (partyContextSpread, error) {
		var s partyContextSpread
		return s, r.Scan(&s.PartyID, &s.Contexts)
	})
	if err != nil {
		return nil, 0, err
	}
	var distinctContexts int
	if err := q.QueryRow(ctx, `
		SELECT count(DISTINCT u.context_id)
		FROM claims c JOIN units u ON u.id = c.unit_id
		WHERE u.context_id = $1`, contextID).Scan(&distinctContexts); err != nil {
		return nil, 0, err
	}
	return spread, distinctContexts, nil
}

// registryReuse is g4_7's endpoint: GET /v1/registry-reuse.
func (h *handlers) registryReuse(w http.ResponseWriter, r *http.Request) {
	if !requirePrivateCaller(w, r, h.d) {
		return
	}
	if !h.authorizeContext(w, r, r.URL.Query().Get("contextId")) {
		return
	}
	spread, distinctContexts, err := contextSpreadPerParty(r.Context(), h.d.DB.Q(), r.URL.Query().Get("contextId"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "read claim/unit context spread", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, reuseMetric(spread, distinctContexts))
}
