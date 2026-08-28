// Command payments is the payments application — one service since #129.
//
// It answers both of the application's questions: "is it true?" (the
// confirmation window, its four exits, the sweep) and "what should be paid,
// what was, and where is the difference?" (instructions, holds,
// reconciliation). It is the Trusted Payments profile's runtime — a deployment
// that never pays anyone simply does not run it (§2.1) — and it is an
// application on the CREST substrate, not part of it (#127): it consumes
// evidence, notify and verification through their public service APIs, and
// nothing beneath it knows a window or a rail exists.
//
// The invariants that live here: every confirmation-window exit releases
// payment — confirm, dispute, auto-confirm, supervisor-assisted, all four; a
// dispute contests the record, not the money. And every held payment has a
// reason with an owner, which the schema enforces as a constraint rather than
// a convention.
package main

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

func main() {
	rail := client.New(config.Str("RAIL_URL", "http://mock-rail:8080"))
	notify := client.New(config.Str("NOTIFY_URL", "http://notify:8080"))
	// The window's exit and the instruction's creation are one service now,
	// but the release still crosses the outbox and this hop on purpose:
	// at-least-once delivery with an idempotent consumer survived the merge,
	// and a crash between exit and instruction still cannot lose a payment.
	self := client.New(config.Str("SELF_URL", "http://localhost:8080"))

	service.Main("payments", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes: func(mux *http.ServeMux, d service.Deps) {
			routes(mux, d)
			windowRoutes(mux, d)
		},
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
					// Idempotent on the claim at the far end; the relay is
					// at-least-once, and a redelivered release must not pay twice.
					return self.Do(ctx, "POST", "/internal/instructions", json.RawMessage(payload), nil)
				case topicRailSend:
					return sendToRail(ctx, d, rail, payload)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	})
}
