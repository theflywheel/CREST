package main

import (
	"errors"
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

// Closing a duplicate hold (§4, W7).
//
// A hold is what the registry writes instead of guessing: two parties carry the
// same identifier, so no evidence joining on it can be attributed, and a person
// has to say which of the two situations this is. Until now nobody could —
// `GET /v1/holds` listed them and nothing closed them.
//
// That has a consequence worth stating plainly rather than discovering later.
// `merges_without_confirmation = 0` has been true since the metric was written,
// because merging was impossible. A control that holds because the action does
// not exist has not been tested; it has been avoided. Now that merging exists,
// the number means what it claims to, and mergesWithoutConfirmation() computes
// it from the same rows the merges are recorded in.
//
// Two decisions, and only one of them takes anything away from anybody:
//
//   - `distinct` — two different people who share a phone number, which is
//     ordinary in a household. The key is removed from every candidate but the
//     one it belongs to, and nothing else about anybody changes. No worker
//     confirmation is required, because asking one worker to ratify a fact
//     about another is not consent, it is disclosure.
//   - `merge` — one person recorded twice. §4 gives this to the custodian *and*
//     to the worker's confirmation, and it is the harder of the two to undo:
//     unlike a wrong record, which the worker can dispute, a wrong merge alters
//     who the system thinks they are.

type holdDecision struct {
	Decision string `json:"decision"`
	// PartyID is who the identifier belongs to: the survivor for a merge, and
	// the rightful holder for a distinct.
	PartyID    string `json:"partyId"`
	ResolvedBy string `json:"resolvedByPartyId"`
	// Confirmation is the worker's, for a merge. Required there and refused for
	// a distinct.
	ConfirmedBy        string `json:"confirmedByPartyId"`
	ConfirmationMethod string `json:"confirmationMethod"`
}

func (h *handlers) resolveHold(w http.ResponseWriter, r *http.Request) {
	var body holdDecision
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	if body.Decision != "merge" && body.Decision != "distinct" {
		httpx.WriteError(w, http.StatusBadRequest, "unknown_decision",
			"decision must be \"merge\" (one person recorded twice) or \"distinct\" "+
				"(two people sharing an identifier); %q is neither", body.Decision)
		return
	}
	if body.PartyID == "" || body.ResolvedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"partyId and resolvedByPartyId are both required: a hold closed by nobody is "+
				"indistinguishable from one the system closed itself")
		return
	}
	switch {
	case body.Decision == "merge" && (body.ConfirmedBy == "" || body.ConfirmationMethod == ""):
		// The refusal §4 exists for. Merging on a custodian's judgement alone
		// is exactly what W7 forbids, and the API must not have a shape that
		// allows it — an unconfirmed merge should be impossible to express,
		// not merely discouraged in a comment.
		httpx.WriteError(w, http.StatusBadRequest, "merge_needs_confirmation",
			"a merge needs confirmedByPartyId and confirmationMethod: only the worker's "+
				"confirmation makes two records one person (§4). A custodian's judgement "+
				"alone is what merges_without_confirmation counts")
		return
	case body.Decision == "distinct" && body.ConfirmedBy != "":
		httpx.WriteError(w, http.StatusBadRequest, "distinct_needs_no_confirmation",
			"a distinct decision takes nothing away from anybody, so it needs no "+
				"confirmation; asking one worker to ratify a fact about another is "+
				"disclosure, not consent")
		return
	}

	// The named custodian must be the caller, or somebody permitted to act
	// for them (#102): a merge alters who the system thinks somebody is, and
	// until now nothing established that resolvedByPartyId was anyone at all.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.ResolvedBy, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}

	now := h.d.Clock.Now()
	var out map[string]any
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		hold, err := openHold(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !contains(hold.Candidates, body.PartyID) {
			return errNotACandidate
		}
		// A survivor that has itself been merged away would build a chain the
		// custodian did not intend and cannot see. Refused rather than
		// followed: they should resolve onto the party that is actually still
		// there.
		into, err := mergedInto(r.Context(), tx, body.PartyID)
		if err != nil {
			return err
		}
		if into != nil {
			return errSurvivorIsMerged
		}

		merged := []string{}
		for _, candidate := range hold.Candidates {
			if candidate == body.PartyID {
				continue
			}
			if body.Decision == "merge" {
				if err := mergeParty(r.Context(), tx, candidate, body.PartyID, now); err != nil {
					return err
				}
				merged = append(merged, candidate)
				continue
			}
			// distinct: the identifier belongs to one of them, so it is taken
			// off the others. Their other keys, and everything else about
			// them, are untouched — they are a different person, not a wrong
			// record.
			if err := removeKey(r.Context(), tx, candidate, hold.KeyKind, hold.KeyValue); err != nil {
				return err
			}
		}
		if err := markHoldResolved(r.Context(), tx, hold.ID, body.Decision, body.PartyID,
			body.ResolvedBy, body.ConfirmedBy, body.ConfirmationMethod, now); err != nil {
			return err
		}
		out = map[string]any{
			"holdId":     hold.ID,
			"decision":   body.Decision,
			"partyId":    body.PartyID,
			"resolvedBy": body.ResolvedBy,
			"resolvedAt": now,
			"merged":     merged,
		}
		if body.Decision == "merge" {
			out["confirmedBy"] = body.ConfirmedBy
			out["confirmationMethod"] = body.ConfirmationMethod
		}
		return nil
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no open hold with that id; it may already have been resolved")
		return
	case errors.Is(err, errNotACandidate):
		httpx.WriteError(w, http.StatusConflict, "not_a_candidate",
			"%s is not one of the parties this hold is between; a hold is closed by choosing "+
				"among its candidates, not by naming a third party", body.PartyID)
		return
	case errors.Is(err, errSurvivorIsMerged):
		httpx.WriteError(w, http.StatusConflict, "survivor_already_merged",
			"%s has itself been merged into another party; resolve onto the one that is "+
				"still there", body.PartyID)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "resolve hold", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

var (
	errNotACandidate    = errors.New("party is not a candidate on this hold")
	errSurvivorIsMerged = errors.New("survivor has itself been merged")
)

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// mergeMetrics answers the monitored invariant §4 names, as a number a test can
// assert on rather than a claim a person can make.
func (h *handlers) mergeMetrics(w http.ResponseWriter, r *http.Request) {
	n, err := mergesWithoutConfirmation(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "count unconfirmed merges", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"mergesWithoutConfirmation": n,
		"because": "every merge carries the worker's confirmation, and the API has no shape " +
			"that expresses one without it (§4)",
	})
}
