package attestation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func legacyWindowNeedsReview(w Window) bool {
	return w.Open() && w.reviewTokenHash == nil && w.AcknowledgedAt == nil
}

// adoptLegacyOpenWindows restores the review capability for windows copied
// from the former payments service. Older rows have no token or notification
// outbox entry. Each repair updates the row and enqueues its notification in
// one transaction, so startup cannot expose a new token without its matching
// review message (or vice versa).
func adoptLegacyOpenWindows(ctx context.Context, d service.Deps) error {
	if d.DB == nil {
		return fmt.Errorf("adopt legacy windows: database unavailable")
	}
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		rows, err := tx.Query(ctx, `
			SELECT claim_id, party_id, context_id, closes_at
			FROM windows
			WHERE exit_route IS NULL AND review_token_hash IS NULL
			  AND acknowledged_at IS NULL
			FOR UPDATE`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var claimID, partyID, contextID string
			var closesAt = d.Clock.Now()
			if err := rows.Scan(&claimID, &partyID, &contextID, &closesAt); err != nil {
				return err
			}
			token, err := newReviewToken()
			if err != nil {
				return fmt.Errorf("generate legacy review token: %w", err)
			}
			digest := sha256.Sum256([]byte(token))
			hash := base64.RawURLEncoding.EncodeToString(digest[:])
			if _, err := tx.Exec(ctx, `
				UPDATE windows
				SET review_token_hash = $2, reach = NULL, reach_detail = NULL,
					notified_at = NULL
				WHERE claim_id = $1 AND exit_route IS NULL
				  AND review_token_hash IS NULL AND acknowledged_at IS NULL`, claimID, hash); err != nil {
				return err
			}
			notice := claimNotification{
				ClaimID: claimID, PartyID: partyID, ContextID: contextID,
				ClosesAt: closesAt, AcknowledgementToken: token,
			}
			raw, err := json.Marshal(notice)
			if err != nil {
				return err
			}
			// Migration 0004 may have copied an old pending notification
			// without a review token. Repair that row in place when possible,
			// avoiding two messages for the same review window.
			updated, err := tx.Exec(ctx, `
				UPDATE outbox SET payload = $2
				WHERE topic = $1 AND payload->>'claimId' = $3
				  AND delivered_at IS NULL AND COALESCE(payload->>'ackToken', '') = ''`,
				topicNotifyClaim, raw, claimID)
			if err != nil {
				return err
			}
			if updated == 0 {
				if err := store.Enqueue(ctx, tx, topicNotifyClaim, notice); err != nil {
					return err
				}
			}
		}
		return rows.Err()
	})
}
