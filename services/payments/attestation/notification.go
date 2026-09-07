package attestation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func configuredNotifier() (notify.Sender, error) { return notify.Configured() }

func sendClaimNotification(ctx context.Context, d service.Deps, sender notify.Sender,
	parties *client.Client, payload json.RawMessage) error {
	var n claimNotification
	if err := json.Unmarshal(payload, &n); err != nil {
		return err
	}
	if sender == nil {
		return recordNotificationFailure(ctx, d.DB, n.ClaimID,
			"no notification provider is configured; worker review remains unopened", d.Clock.Now())
	}
	var party schema.Party
	if err := parties.Get(ctx, "/internal/parties/"+url.PathEscape(n.PartyID), &party); err != nil {
		return recordNotificationFailure(ctx, d.DB, n.ClaimID,
			"worker contact could not be read from the parties service", d.Clock.Now())
	}
	var email string
	for _, route := range party.ContactRoutes {
		if string(route.Kind) == "email" && strings.TrimSpace(route.Value) != "" {
			email = route.Value
			break
		}
	}
	if email == "" {
		return recordNotificationFailure(ctx, d.DB, n.ClaimID,
			"worker has no email contact route configured", d.Clock.Now())
	}
	base := strings.TrimRight(config.Str("NOTIFY_ACK_BASE_URL", "http://localhost:8080"), "/")
	ackURL := base + "/worker/#/review/" + url.PathEscape(n.ClaimID) + "?token=" + url.QueryEscape(n.AcknowledgementToken)
	result, err := sender.Send(ctx, notify.Message{
		To: email, Subject: "CREST work record review",
		Body:           "Your work record is ready to review. Open this link to authenticate and acknowledge it:\n" + ackURL,
		Acknowledgment: ackURL,
	})
	if err != nil || !result.Accepted {
		detail := "notification provider did not accept the message"
		if err != nil {
			detail = err.Error()
		}
		return recordNotificationFailure(ctx, d.DB, n.ClaimID, detail, d.Clock.Now())
	}
	detail := "notification accepted by configured provider; worker acknowledgement is still required"
	if result.ProviderID != "" {
		detail += " (provider " + result.ProviderID + ")"
	}
	if err := recordNotificationAccepted(ctx, d.DB, n.ClaimID, detail, d.Clock.Now()); err != nil {
		return err
	}
	// The acknowledgement token is a bearer capability. Once the provider has
	// accepted the notification, retaining it in a delivered outbox payload
	// creates a durable replay surface for anyone who can inspect the queue.
	// Scrub only this service's notification row; the shared outbox contract is
	// intentionally untouched.
	return scrubDeliveredNotificationToken(ctx, d.DB, n.ClaimID)
}

func recordNotificationAccepted(ctx context.Context, db *store.DB, claimID, detail string, at time.Time) error {
	if db == nil {
		return fmt.Errorf("record notification acceptance: database unavailable")
	}
	return db.InTx(ctx, func(tx store.Querier) error {
		return recordNotificationAttempt(ctx, tx, claimID, detail, at)
	})
}

func recordNotificationFailure(ctx context.Context, db *store.DB, claimID, detail string, at time.Time) error {
	if db == nil {
		return fmt.Errorf("record notification failure: database unavailable")
	}
	terminal := false
	if err := db.InTx(ctx, func(tx store.Querier) error {
		win, err := getWindow(ctx, tx, claimID, true)
		if err != nil {
			return err
		}
		// A provider callback can race a worker acknowledgement or another
		// exit. Once the record is acknowledged/closed, a late failure must
		// neither downgrade reach nor keep an already-complete outbox item
		// retrying forever.
		if !notificationFailureApplicable(win) {
			terminal = true
			return nil
		}
		return recordReach(ctx, tx, claimID, "unreached", detail, at)
	}); err != nil {
		return err
	}
	if terminal {
		return scrubDeliveredNotificationToken(ctx, db, claimID)
	}
	return fmt.Errorf("notification not delivered: %s", detail)
}

func notificationFailureApplicable(win Window) bool {
	return win.AcknowledgedAt == nil && win.ExitRoute == nil
}

func scrubDeliveredNotificationToken(ctx context.Context, db *store.DB, claimID string) error {
	if db == nil {
		return fmt.Errorf("scrub notification token: database unavailable")
	}
	return db.InTx(ctx, func(tx store.Querier) error {
		_, err := tx.Exec(ctx, `
			UPDATE outbox
			SET payload = payload - 'ackToken'
			WHERE topic = $1 AND payload->>'claimId' = $2
			  AND claimed_at IS NOT NULL AND delivered_at IS NULL`, topicNotifyClaim, claimID)
		return err
	})
}
