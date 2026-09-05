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
	"net/http"

	"github.com/theflywheel/crest/adapters/builtin"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service is this member's wiring, composed into the core binary (#150).
func Service() service.Options {
	confirmation := client.New(config.Str("CONFIRMATION_URL", "http://payments:8080"))
	connectors := builtin.Registry()

	return service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes: func(mux *http.ServeMux, d service.Deps) {
			routes(mux, d, connectors)
		},

		// Two side effects, both delivered from the outbox rather than inline.
		// A claim that exists with no window is a worker who is never asked and
		// never paid (W2, W4); a source outage nobody was told about is work
		// that stops being recorded with nobody noticing (#22).
		Deliver: func(d service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicClaimCreated:
					return confirmation.Do(ctx, "POST", "/v1/windows", json.RawMessage(payload), nil)
				case topicSourceQuiet:
					// Notifications are dropped (#150): the outage is still
					// detected and still lands in the log loudly, but nobody
					// is paged. The send returns here when a channel exists.
					return func() error {
						var q struct {
							SystemRef string `json:"systemRef"`
						}
						_ = json.Unmarshal(payload, &q)
						d.Log.Warn("evidence source is quiet and nobody was told: no notification channel (#150)",
							"systemRef", q.SystemRef)
						return nil
					}()
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	}
}
