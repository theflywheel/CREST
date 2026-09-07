// Command evidence is a CREST service.
//
// Answers "what happened?" — adapters in, units and claims out, with validation
// and an unclear queue (§13).
//
// It is where the unit/claim split is created and therefore where it is kept
// honest: nothing here accepts "activity plus actor" as one write. A row that
// cannot be attributed still produces a Unit is a design choice this service
// deliberately does *not* make — see the unclear queue, and the note in
// ingest.go about why.
package evidence

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/clockctl"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service is this member's wiring, composed into the core binary (#150).
func Service() service.Options {
	// The confirmation window is the payments application's since #127, and
	// since the attestation member moved there it is a different process.
	// This client is the only thing in the infrastructure that knows a window
	// exists at all, and it does not: it delivers `claim.created` to whatever
	// CONFIRMATION_URL names and has no opinion about what happens next.
	confirmation := client.New(config.Str("CONFIRMATION_URL", "http://payments:8080"))

	notifier, err := notify.Configured()
	if err != nil {
		panic(err)
	}
	parties := client.New(config.Str("PARTIES_URL", ""))
	return service.Options{
		// The source-quiet monitor is this member's own scheduled behaviour
		// (#22): a feed that stops sending has to be noticed by a clock, and
		// "a daily source went 25 hours without a batch" is asserted by moving
		// time rather than by waiting a day (docs/TESTING.md).
		//
		// Declared again after #127 briefly took it away. #127's ruling — the
		// confirmation window is programme policy and belongs to the payments
		// application — is untouched by this; what was wrong was the corollary
		// that a window is the ONLY reason a CREST process wants driveable
		// time. Ruled on 2026-09-07 (#215): the seam is a non-production
		// harness surface, refused outside local/test by pkg/clockctl and
		// again by pkg/service's deployment refusal, so declaring it leaks no
		// programme policy into the substrate. What #127 forbade is the window
		// living here, and no window does.
		ClockSeam: clockctl.Seam,
		OnStart: func(ctx context.Context, d service.Deps) error {
			every, err := config.Duration("SOURCE_MONITOR_EVERY", time.Minute)
			if err != nil || every <= 0 {
				return fmt.Errorf("SOURCE_MONITOR_EVERY must be positive")
			}
			go monitorLoop(ctx, d, every)
			return nil
		},
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,

		// Two side effects, both delivered from the outbox rather than inline.
		// A claim that exists with no window is a worker who is never asked and
		// never paid (W5–W6); a source outage nobody was told about is work
		// that stops being recorded with nobody noticing (#22).
		Deliver: func(d service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicClaimCreated:
					return confirmation.Do(ctx, "POST", "/internal/windows", json.RawMessage(payload), nil)
				case topicSourceQuiet:
					return notifyQuietSource(ctx, d, parties, notifier, payload)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	}
}
