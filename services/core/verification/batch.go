package verification

// Batch verification (§16, #107): the bounded door for a need that would
// otherwise arrive through a loop.
//
// An employer verifying a cohort will script the single-check endpoint if no
// batch exists — the same disclosure, invisible. So the batch exists, and it
// is bounded in the two ways the ruling names:
//
//   - Every batch declares a purpose. That requirement is L1 and not
//     negotiable; a batch of checks with no stated purpose is a bulk export.
//     The purpose is short human-readable text, not a code, because it ends up
//     in front of the worker in their check trail and a worker should not need
//     a codebook to learn why they were checked.
//   - Every batch is size-capped. The cap itself is L2 — a national payroll
//     run and a three-NGO pilot reasonably disagree about the number — with an
//     L1 default a deployment can change but not remove: a non-positive value
//     is refused at startup below rather than read as "unlimited".
//
// There are deliberately NO aggregate answers — no counts, no summaries, only
// per-credential verdicts. J9/J11's small-population worry (a batch of three
// in a village of four discloses the fourth by omission) applies to answers
// about populations; per-credential verdicts about credentials the caller
// already holds add nothing to what showing each credential singly would, so
// the floor problem never arises here.
//
// Each credential in a batch gets its own entry in the presentation trail.
// One entry per worker, never one per batch: "who checked me" must answer the
// same whether the check arrived alone or in a thousand.

import (
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
)

const (
	defaultBatchCap = 100
	// The purpose bounds are L1: long enough that "annual audit, Q3 payroll,
	// Riverside district" fits, short enough that nobody pastes a document.
	purposeMinChars = 10
	purposeMaxChars = 200
)

// batchCap reads the deployment's cap. Changing it is legitimate; removing it
// is not, so zero and negative values are refused rather than read as
// "unbounded" — a cap that can be configured away is a warning, not a cap.
func batchCap(log *slog.Logger) int {
	raw := config.Str("CREST_VERIFY_BATCH_CAP", "")
	if raw == "" {
		return defaultBatchCap
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Error("CREST_VERIFY_BATCH_CAP is not a positive integer; using the default",
			"value", raw, "default", defaultBatchCap)
		return defaultBatchCap
	}
	return n
}

func (h *handlers) verifyBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credentials        []map[string]any `json:"credentials"`
		RequestedByPartyID string           `json:"requestedByPartyId"`
		Purpose            string           `json:"purpose"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if n := utf8.RuneCountInString(req.Purpose); n < purposeMinChars || n > purposeMaxChars {
		httpx.WriteError(w, http.StatusBadRequest, "purpose_required",
			"a batch check must say what it is for, in %d-%d characters a worker can read "+
				"in their check trail; got %d", purposeMinChars, purposeMaxChars, n)
		return
	}
	if req.RequestedByPartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "requester_required",
			"a batch check must say who is asking: the check trail answers \"who checked me\", "+
				"and for a batch that answer must not be nobody")
		return
	}
	if len(req.Credentials) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "no credentials to verify")
		return
	}
	if limit := batchCap(h.d.Log); len(req.Credentials) > limit {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "batch_over_cap",
			"this batch holds %d credentials and the deployment's cap is %d; "+
				"split it, or take up the cap with whoever operates this deployment",
			len(req.Credentials), limit)
		return
	}

	verdicts := make([]Verdict, 0, len(req.Credentials))
	now := h.d.Clock.Now()
	for _, doc := range req.Credentials {
		verdict, subjectRef, credID := h.assess1(r.Context(), doc)
		// One trail entry per worker, unconditionally — including for a
		// credential that failed to verify, because "somebody tried to check
		// me with a bad document" is also a fact about the worker's record.
		if err := h.record(r.Context(), presentation{
			ID: id.New(h.d.Clock, "presentation"), CredentialID: credID,
			SubjectRef: subjectRef, RequestedBy: req.RequestedByPartyID,
			Purpose: req.Purpose, Scope: "scoped", Outcome: outcomeOf(verdict),
			Tier: verdict.Tier, CreatedAt: now,
		}); err != nil {
			httpx.Fail(w, h.d.Log, "record presentation", err)
			return
		}
		verdicts = append(verdicts, verdict)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"verdicts": verdicts, "count": len(verdicts),
	})
}
