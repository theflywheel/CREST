package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Working the unclear queue (#25).
//
// Until this existed the queue could be listed and never worked: 0001 gave it a
// `record` column so a row could be "re-attributed once the person is
// identified rather than asked for again", and `resolved_at`/`resolved_to` to
// close it with, and nothing ever wrote any of them. In the PoC that is seven
// rows, three of them real work by real people whose phone the registry did not
// recognise. In a pilot it is three workers who did the work and get nothing,
// with a queue that only ever grows.
//
// Three decisions are built into what follows, and each was taken deliberately:
//
//   - **The T=7 window starts at resolution, not at ingest.** A worker cannot
//     object to a record they were never shown. Starting the clock at ingest
//     would auto-confirm work resolved three weeks later before the worker had
//     seen it once, and auto-confirmation on someone who never heard is silence
//     the system manufactured.
//   - **Re-attribution needs no confirmation step of its own.** Attaching work
//     to a party creates a claim, and every claim already passes through a
//     window where the worker may dispute it. A second gate would duplicate the
//     one that exists — and would leave the work unattached while an
//     unreachable worker fails to answer, which is the exact failure the
//     supervisor-assisted exit was built to avoid. This is the opposite of the
//     merge rule in §4, and for a reason: a merge alters who someone *is*, and
//     is not undoable by disputing a record.
//   - **Only an `unattributed` row can be resolved.** 0005 says why for each of
//     the other kinds. The short version: every other kind failed the evidence
//     contract, and attaching a person to it would launder a record that failed
//     the contract into the ledger wearing a worker's name.

// resolveUnclear attaches a named worker to a row nobody could attribute.
//
// It deliberately does not re-fetch the definition. The row is `unattributed`,
// which means it already passed the contract, activity and outcome-unit checks
// against the version in force when the batch arrived; the batch pinned that
// version and the unit is written against it. Re-checking against whatever is
// active today would fail rows for having been ingested before a definition was
// revised — the stranding that pinning at ingestion exists to prevent (§7).
func (h *handlers) resolveUnclear(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PartyID    string `json:"partyId"`
		ResolvedBy string `json:"resolvedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &body) {
		return
	}
	for name, value := range map[string]string{
		"partyId": body.PartyID, "resolvedByPartyId": body.ResolvedBy,
	} {
		if value == "" {
			httpx.WriteError(w, http.StatusBadRequest, "missing_field",
				"%s is required: a re-attribution with no named resolver is the same shape of "+
					"unaccountable as a held payment with no owner", name)
			return
		}
	}
	// The resolver is the caller (#102): the permits check below asks whether
	// resolvedBy MAY do this, and this asks whether resolvedBy IS the person
	// asking — the two halves #89 separated.
	if _, ok := identity.Authorize(w, r, h.d.Log, body.ResolvedBy, "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}

	rowID := r.PathValue("id")
	now := h.d.Clock.Now()

	// Read the row and its batch outside the resolving transaction only to
	// learn the context: the authorisation and consent checks are network calls
	// to the registry, and holding a row lock across them would let a slow
	// registry block the queue. The row is re-read under FOR UPDATE inside the
	// transaction, and every check that matters is repeated there.
	row, batch, err := h.unclearWithBatch(r.Context(), rowID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found",
			"no open unclear row %s: it may already have been resolved", rowID)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "read the unclear row", err)
		return
	}

	if row.Kind != unclearUnattributed {
		httpx.WriteError(w, http.StatusConflict, "not_reattributable",
			"this row is %q, not %q: %s. Only a row whose record was sound and whose worker "+
				"was unknown can be attached to a person",
			row.Kind, unclearUnattributed, row.Reason)
		return
	}
	if len(row.Record) == 0 {
		httpx.WriteError(w, http.StatusConflict, "no_record",
			"this row has no parsed record, so there is nothing to attach a worker to")
		return
	}

	permitted, err := h.in.permitsFunction(r.Context(), body.ResolvedBy,
		"resolve-unclear-evidence", batch.ContextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check the resolver's authorization", err)
		return
	}
	if !permitted {
		httpx.WriteError(w, http.StatusForbidden, "not_authorised",
			"%s is not authorised to resolve unclear evidence in %s", body.ResolvedBy, batch.ContextID)
		return
	}

	// The same rule ingestion applies, applied again here: a worker who has
	// withdrawn enrolment consent has asked us to stop recording evidence about
	// them (§9), and resolving a row onto them would record it anyway. NONE
	// still passes, for the same migration-gap reason ingestion gives.
	consent, err := h.in.enrolmentConsent(r.Context(), body.PartyID, batch.ContextID)
	switch {
	case errors.Is(err, errNoSuchParty):
		httpx.WriteError(w, http.StatusBadRequest, "no_such_party",
			"the registry holds no party %s", body.PartyID)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "read the worker's enrolment consent", err)
		return
	case consent == "WITHDRAWN":
		httpx.WriteError(w, http.StatusConflict, "consent_withdrawn",
			"%s has withdrawn enrolment consent in %s, so no new evidence about them is "+
				"recorded (§9)", body.PartyID, batch.ContextID)
		return
	}

	var rec schema.CanonicalWorkEvidenceRecord
	if err := json.Unmarshal(row.Record, &rec); err != nil {
		httpx.Fail(w, h.d.Log, "read the stored record", err)
		return
	}

	unit := schema.Unit{
		ID:         id.New(h.d.Clock, "unit"),
		Definition: schema.VersionedRef{ID: batch.DefinitionID, Version: batch.DefinitionVersion},
		ContextID:  batch.ContextID,
		Outcome:    rec.Outcome,
		Period:     rec.Period,
		Geography:  rec.Geography,
		Enrichment: rec.Enrichment,
		Provenance: rec.Provenance,
		// The work's own timestamps are in Period. CreatedAt is when the record
		// entered CREST, which is when its batch arrived — not now. Stamping it
		// now would make late-resolved work look freshly reported in every
		// count of how quickly evidence turns into payment.
		CreatedAt: row.CreatedAt,
	}
	claim := schema.Claim{
		ID:      id.New(h.d.Clock, "claim"),
		UnitID:  unit.ID,
		PartyID: body.PartyID,
		State:   schema.ClaimStateDRAFT,
		Matched: &schema.ClaimMatched{
			// Named by a person rather than matched on a key, and recorded as
			// such: a verifier reading this claim should be able to see that
			// the attribution was somebody's decision. Confidence is 1 because
			// a person asserted it, not because an algorithm scored it.
			Key:        schema.ClaimMatchedKeyManual,
			Confidence: 1,
		},
		CreatedAt: now,
	}

	var created bool
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		locked, err := getOpenUnclear(r.Context(), tx, rowID)
		if err != nil {
			return err
		}
		if locked.Kind != unclearUnattributed {
			return errNotReattributable
		}
		unitID, err := insertUnit(r.Context(), tx, locked.BatchID, unit, dedupeKey(unit, rec))
		if err != nil {
			return err
		}
		// The unit already existed, so this work is already in the ledger —
		// which happens when the same row arrived twice and one copy was
		// resolved first. The claim's uniqueness on (unit_id, party_id) then
		// refuses the second attribution to the same worker, and the row is
		// still closed: it has been dealt with, and leaving it open would put
		// it back in front of somebody who would resolve it again.
		unit.ID = unitID
		claim.UnitID = unitID
		created, err = insertClaim(r.Context(), tx, claim)
		if err != nil {
			return err
		}
		claimID := claim.ID
		if !created {
			claimID = ""
		}
		if err := markUnclearResolved(r.Context(), tx, rowID, body.PartyID,
			body.ResolvedBy, claimID, now); err != nil {
			return err
		}
		if !created {
			return nil
		}
		// The window request carries `now`, so the seven days run from the
		// resolution. Confirmation opens the window against its own clock at
		// the moment it handles this message, which is the same decision from
		// the other side.
		return store.Enqueue(r.Context(), tx, topicClaimCreated, windowRequest{
			ClaimID:      claim.ID,
			UnitID:       unitID,
			PartyID:      claim.PartyID,
			ContextID:    unit.ContextID,
			DefinitionID: batch.DefinitionID,
			Version:      batch.DefinitionVersion,
			CreatedAt:    now,
		})
	})
	switch {
	case errors.Is(err, errNotReattributable):
		httpx.WriteError(w, http.StatusConflict, "not_reattributable",
			"this row stopped being re-attributable while it was being resolved")
		return
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusConflict, "already_resolved",
			"somebody else resolved %s first", rowID)
		return
	case err != nil:
		httpx.Fail(w, h.d.Log, "resolve the unclear row", err)
		return
	}

	out := map[string]any{
		"unclearRowId": rowID,
		"partyId":      body.PartyID,
		"resolvedBy":   body.ResolvedBy,
		"resolvedAt":   now,
		"unitId":       unit.ID,
	}
	if created {
		out["claimId"] = claim.ID
		out["confirmationWindowOpensAt"] = now
	} else {
		out["note"] = "this work was already claimed by this worker; the row is closed and no " +
			"second claim was created"
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

var errNotReattributable = errors.New("row is not re-attributable")

func (h *handlers) unclearWithBatch(ctx context.Context, rowID string) (UnclearRow, Batch, error) {
	row, err := getOpenUnclear(ctx, h.d.DB.Q(), rowID)
	if err != nil {
		return UnclearRow{}, Batch{}, err
	}
	batch, err := getBatch(ctx, h.d.DB.Q(), row.BatchID)
	return row, batch, err
}

var errNoSuchParty = errors.New("no such party")

// enrolmentConsent asks the registry what a named worker's enrolment consent is
// in one context. Derived there rather than cached here, for the reason the
// registry's own comment gives: a cached "consented" is a withdrawal that has
// not taken effect yet.
func (in *ingestor) enrolmentConsent(ctx context.Context, partyID, contextID string) (string, error) {
	var out struct {
		EnrolmentConsent string `json:"enrolmentConsent"`
	}
	err := in.registry.Get(ctx, fmt.Sprintf("/internal/parties/%s/enrolment-consent?contextId=%s",
		urlSafe(partyID), urlSafe(contextID)), &out)
	if client.Code(err) == http.StatusNotFound {
		return "", errNoSuchParty
	} else if err != nil {
		return "", fmt.Errorf("registry could not answer for %s: %w", partyID, err)
	}
	return out.EnrolmentConsent, nil
}

// permitsFunction is permits() with the function named rather than fixed. The
// original hard-codes submit-work-evidence because that is the only thing it
// ever asks about; resolving a row is a different function held by a different
// person, and conflating the two would let anyone who can submit a batch also
// decide whose work it was.
func (in *ingestor) permitsFunction(ctx context.Context, partyID, function, contextID string) (bool, error) {
	var out struct {
		Permitted bool `json:"permitted"`
	}
	err := in.registry.Get(ctx, fmt.Sprintf(
		"/internal/authorizations/permits?partyId=%s&function=%s&contextId=%s",
		urlSafe(partyID), urlSafe(function), urlSafe(contextID)), &out)
	if err != nil {
		return false, fmt.Errorf("registry could not check the authorization: %w", err)
	}
	return out.Permitted, nil
}
