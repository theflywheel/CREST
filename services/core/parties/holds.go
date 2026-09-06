package parties

import (
	"errors"
	"net/http"
	"strings"

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
	ResolvedBy string `json:"resolvedByPartyId"` // legacy input, cross-checked against caller
}

type holdConfirmationRequest struct {
	SurvivorPartyID    string `json:"survivorPartyId"`
	ConfirmationMethod string `json:"confirmationMethod"`
	EvidenceRef        string `json:"evidenceRef,omitempty"`
}

// confirmHold is a separate append-only worker action. The authenticated
// caller, never a body field, is the confirmer.
func (h *handlers) confirmHold(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	var body holdConfirmationRequest
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	body.SurvivorPartyID = strings.TrimSpace(body.SurvivorPartyID)
	body.ConfirmationMethod = strings.TrimSpace(body.ConfirmationMethod)
	if body.SurvivorPartyID == "" || body.ConfirmationMethod == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field", "survivorPartyId and confirmationMethod are required")
		return
	}
	caller, ok := actualCaller(r)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "worker_identity_required", "a merge confirmation must come from an enrolled worker identity")
		return
	}
	now := h.d.Clock.Now()
	var out map[string]any
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		hold, err := openHold(r.Context(), tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if !contains(hold.Candidates, body.SurvivorPartyID) {
			return errNotACandidate
		}
		if !contains(hold.Candidates, caller) {
			return errConfirmationNotCandidate
		}
		if err := insertHoldConfirmation(r.Context(), tx, hold.ID, body.SurvivorPartyID, caller, body.ConfirmationMethod, body.EvidenceRef, now); err != nil {
			if store.IsUniqueViolation(err) {
				got, method, evidence, readErr := holdConfirmation(r.Context(), tx, hold.ID, body.SurvivorPartyID)
				if readErr == nil && got == caller && method == body.ConfirmationMethod && evidence == body.EvidenceRef {
					out = map[string]any{"holdId": hold.ID, "survivorPartyId": body.SurvivorPartyID, "confirmedByPartyId": caller, "confirmationMethod": method, "evidenceRef": evidence}
					return nil
				}
				return errConfirmationConflict
			}
			return err
		}
		out = map[string]any{"holdId": hold.ID, "survivorPartyId": body.SurvivorPartyID, "confirmedByPartyId": caller, "confirmationMethod": body.ConfirmationMethod, "evidenceRef": body.EvidenceRef, "confirmedAt": now}
		return nil
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no open hold with that id")
	case errors.Is(err, errNotACandidate), errors.Is(err, errConfirmationNotCandidate):
		httpx.WriteError(w, http.StatusForbidden, "worker_not_on_hold", "the authenticated worker is not a candidate on this hold")
	case errors.Is(err, errConfirmationConflict):
		httpx.WriteError(w, http.StatusConflict, "confirmation_conflict", "this hold already has a different worker confirmation")
	case err != nil:
		httpx.Fail(w, h.d.Log, "confirm hold", err)
	default:
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *handlers) resolveHold(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
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
	if body.PartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_field",
			"partyId is required")
		return
	}
	// Read the scope before checking the assignment. A hold is always resolved
	// under the project that created it; an unscoped legacy row is refused.
	holdScope, err := openHold(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no open hold with that id")
		return
	}
	if holdScope.ContextID == "" {
		httpx.WriteError(w, http.StatusConflict, "hold_scope_missing", "this hold has no custodian project scope")
		return
	}
	resolvedBy, ok := requireRegistryCustodian(w, r, h.d, holdScope.ContextID)
	if !ok {
		return
	}
	if body.ResolvedBy != "" && body.ResolvedBy != resolvedBy {
		httpx.WriteError(w, http.StatusForbidden, "actor_mismatch", "resolvedByPartyId must equal the authenticated custodian")
		return
	}

	now := h.d.Clock.Now()
	var out map[string]any
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
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
		confirmedBy, confirmationMethod, evidenceRef, confirmationErr := "", "", "", error(nil)
		if body.Decision == "merge" {
			confirmedBy, confirmationMethod, evidenceRef, confirmationErr = holdConfirmation(r.Context(), tx, hold.ID, body.PartyID)
			if errors.Is(confirmationErr, store.ErrNotFound) {
				return errMergeNeedsWorker
			}
			if confirmationErr != nil {
				return confirmationErr
			}
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
			resolvedBy, confirmedBy, confirmationMethod, now); err != nil {
			return err
		}
		out = map[string]any{
			"holdId":     hold.ID,
			"decision":   body.Decision,
			"partyId":    body.PartyID,
			"resolvedBy": resolvedBy,
			"resolvedAt": now,
			"merged":     merged,
		}
		if body.Decision == "merge" {
			out["confirmedBy"] = confirmedBy
			out["confirmationMethod"] = confirmationMethod
			if evidenceRef != "" {
				out["evidenceRef"] = evidenceRef
			}
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
	case errors.Is(err, errMergeNeedsWorker):
		httpx.WriteError(w, http.StatusConflict, "merge_needs_worker_confirmation",
			"the worker must confirm this exact survivor before the custodian can merge the records")
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "resolve hold", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

var (
	errNotACandidate            = errors.New("party is not a candidate on this hold")
	errSurvivorIsMerged         = errors.New("survivor has itself been merged")
	errConfirmationNotCandidate = errors.New("confirmation caller is not a candidate")
	errConfirmationConflict     = errors.New("hold already has a different confirmation")
	errMergeNeedsWorker         = errors.New("merge needs worker confirmation")
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
	if _, ok := requireRegistryCustodian(w, r, h.d, ""); !ok {
		return
	}
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
