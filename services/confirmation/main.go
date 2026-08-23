// Command confirmation is a CREST service.
//
// Answers "is it true?" — the T=7 window, and issuance through it (§9, §13).
//
// The invariant this service exists to keep is W4: every T=7 exit releases
// payment. Confirm, dispute, auto-confirm, supervisor-assisted — all four. A
// dispute contests the record; it does not withhold the money. Everything about
// the shape here follows from that: the exit and the release are separate
// columns so a missed release is visible, and the release goes through the
// outbox so a crash between the two cannot lose it.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	notify := client.New(config.Str("NOTIFY_URL", "http://notify:8080"))
	payments := client.New(config.Str("PAYMENTS_URL", "http://payments:8080"))

	service.Main("confirmation", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
		Deliver: func(d service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicNotifyClaim:
					// The outcome comes back and is written down. notify
					// answers 201 for a send that failed and for a worker with
					// no reachable route, which is right for the outbox — but
					// it means "delivered" here has never meant "the worker
					// knows". The sweep reads what this records.
					return deliverNotification(ctx, d, notify, payload)
				case topicPaymentRelease:
					// Idempotent on the claim at the far end. The relay is
					// at-least-once, and a redelivered release must not pay twice.
					return payments.Do(ctx, "POST", "/v1/instructions", json.RawMessage(payload), nil)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	})
}
