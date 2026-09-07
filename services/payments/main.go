// Command payments is the payments application — one service since #129.
//
// It answers both of the application's questions: "is it true?" (the
// confirmation window, its four exits, the sweep) and "what should be paid,
// what was, and where is the difference?" (instructions, holds,
// reconciliation). It is the Trusted Payments profile's runtime — a deployment
// that never pays anyone simply does not run it (§2.1) — and it is an
// application on the CREST substrate, not part of it (#127): it consumes
// evidence and verification through their public service APIs, and
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

	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	service.Main("payments", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes: func(mux *http.ServeMux, d service.Deps) {
			routes(mux, d)
		},
		Deliver: func(d service.Deps) store.Deliverer {
			provider := mustConfiguredProvider(d)
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicRailSend:
					return sendToRail(ctx, d, provider, payload)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	})
}
