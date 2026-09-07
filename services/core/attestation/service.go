package attestation

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/clockctl"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service returns the attestation member wiring and its outbox delivery hooks.
func Service() service.Options {
	notifier, err := configuredNotifier()
	if err != nil {
		panic(err)
	}
	verification := client.New(config.Str("VERIFICATION_URL", ""))
	evidence := client.New(config.Str("EVIDENCE_URL", ""))
	parties := client.New(config.Str("PARTIES_URL", ""))
	payments := client.New(config.Str("PAYMENTS_URL", ""))
	return service.Options{
		// The one infrastructure-side mount of the driveable clock, and it is
		// here under protest: this member holds the confirmation window and
		// its sweep, which #127 ruled are the payments application's, not the
		// substrate's. Moving the member is a separate change; until it
		// happens the harness cannot advance a window without advancing core,
		// so the seam stays named here rather than hidden in pkg/service where
		// it silently applied to parties, definitions, evidence and
		// verification too. Delete this line with the member.
		ClockSeam: clockctl.Seam,
		OnStart: func(ctx context.Context, d service.Deps) error {
			return adoptLegacyOpenWindows(ctx, d)
		},
		Migrations: migrations, Dir: "migrations", Routes: windowRoutes,
		Deliver: func(d service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicContestResolution:
					return applyResolution(ctx, d, evidence, verification, payload)
				case topicNotifyClaim:
					return sendClaimNotification(ctx, d, notifier, parties, payload)
				case topicPaymentRelease:
					if !config.MustBool("PAYMENT_SUBSCRIBER_ENABLED", false) {
						// A credential-only deployment deliberately has no payment
						// acceptance boundary. Consume the queue item so it does not
						// retry forever, while leaving payment_released_at NULL for
						// the unreleased reconciliation surface.
						d.Log.Warn("payment subscriber disabled; release remains unreleased")
						return nil
					}
					var release releaseRequest
					if err := json.Unmarshal(payload, &release); err != nil || release.ClaimID == "" {
						return fmt.Errorf("invalid payment release instruction")
					}
					if err := payments.Do(ctx, http.MethodPost, "/internal/instructions", payload, nil); err != nil {
						return err
					}
					// The payment service's successful response is the durable
					// acceptance boundary. Only then can W5–W6's release marker be
					// exposed as complete to reconciliation and operators.
					return markPaymentReleased(ctx, d.DB, release.ClaimID, d.Clock.Now())
				default:
					return fmt.Errorf("unknown attestation event %q", topic)
				}
			}
		},
	}
}
