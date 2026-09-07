// Command payments is the payments application — one service since #129, and
// since #127 the only process that holds a confirmation window.
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
// Two members, one process, one port (#150's Compose). `payments` holds the
// instructions, holds and reconciliation; `attestation` holds the window and
// its exits, moved here from the core infrastructure deployable by #127. They
// are separate members rather than one package because they are separate
// schemas with separate migration chains and separate outboxes, and the split
// is what lets the window's tables keep their names and their rows through
// the move. The boundary that actually moved is the deployable one: what used
// to be evidence talking to a sibling member in the same binary is now
// evidence's outbox delivering over HTTP to another service, with the service
// token, which is what CONFIRMATION_URL has always named.
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

	"github.com/theflywheel/crest/pkg/clockctl"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/services/payments/attestation"
)

//go:embed migrations/*.sql
var migrations embed.FS

// paymentsMember is the instruction/hold/reconciliation half of the
// application. Named rather than inlined so the clock-seam test can ask the
// wiring what it declares without starting a process.
func paymentsMember() service.Options {
	return service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		// The driveable clock is this application's harness surface, not the
		// substrate's (#127) — and since the window moved here, this is the
		// only declaration of it anywhere in the fleet. A window a week long
		// cannot be demonstrated or tested in real time, so the application
		// that owns the window owns the seam that moves time through it; the
		// infrastructure services, which have no window, no longer carry the
		// capability at all and have no /internal/clock to refuse. Declared
		// on this member and not on attestation because a process has one
		// clock: Compose refuses two declarations rather than picking one.
		// Refused outside local/test by pkg/clockctl and again by the
		// deployment refusal in pkg/service.
		ClockSeam: clockctl.Seam,
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
	}
}

func main() {
	service.Compose("payments", []service.Member{
		{Name: "payments", Opts: paymentsMember()},
		// The confirmation window. Its Postgres schema is still `attestation`
		// and its tables still hold every window ever opened; #127 moved which
		// process owns them, not where they live.
		{Name: "attestation", Opts: attestation.Service()},
	})
}
