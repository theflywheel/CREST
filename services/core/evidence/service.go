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
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/notify"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service is this member's wiring, composed into the core binary (#150).
func Service() service.Options {
	confirmation := client.New(config.Str("CONFIRMATION_URL", "http://core:8080"))

	notifier, err := notify.Configured()
	if err != nil {
		panic(err)
	}
	parties := client.New(config.Str("PARTIES_URL", ""))
	return service.Options{
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
		// never paid (W2, W4); a source outage nobody was told about is work
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
