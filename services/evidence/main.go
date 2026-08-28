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
	confirmation := client.New(config.Str("CONFIRMATION_URL", "http://payments:8080"))
	notify := client.New(config.Str("NOTIFY_URL", "http://notify:8080"))

	service.Main("evidence", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,

		// Two side effects, both delivered from the outbox rather than inline.
		// A claim that exists with no window is a worker who is never asked and
		// never paid (W2, W4); a source outage nobody was told about is work
		// that stops being recorded with nobody noticing (#22).
		Deliver: func(_ service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicClaimCreated:
					return confirmation.Do(ctx, "POST", "/v1/windows", json.RawMessage(payload), nil)
				case topicSourceQuiet:
					return notify.Do(ctx, "POST", "/internal/notifications", json.RawMessage(payload), nil)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	})
}
