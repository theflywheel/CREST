package attestation

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service returns the attestation member wiring and its outbox delivery hooks.
//
// This is the confirmation window: its four exits, its sweep, its contests and
// its notifications. #127 ruled all of it the payments application's rather
// than the substrate's — a window's length is programme policy, and two
// deployments can set it differently and both still be CREST — so the member
// is composed into the payments process (services/payments/main.go) and is no
// longer part of the core infrastructure deployable.
//
// Its Postgres schema is still named `attestation` and its tables keep their
// names. That is deliberate: moving the member across a deployable boundary
// must not move a single row of a worker's confirmation history. Both
// processes address the same database (`crest`); what changed is which
// process owns and migrates this schema, which is now payments alone.
//
// Everything it needs from the substrate it asks for over HTTP with the
// service token — evidence, verification and parties by their public
// `/internal` surfaces, exactly as it did before the move, because it always
// held clients rather than in-process handles.
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
		// No ClockSeam here. This member holds the window, but it runs inside
		// the payments process, and a process has one clock: payments declares
		// the seam once, in its own wiring, for the whole application (#127).
		// Two members declaring it is a wiring mistake Compose refuses at
		// startup rather than a merge.
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
