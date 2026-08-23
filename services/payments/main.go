// Command payments is a CREST service.
//
// Answers "what should be paid, what was, and where is the difference?" (§13).
// It is the Trusted Payments profile's runtime — a deployment that never pays
// anyone simply does not run it (§2.1).
//
// Two invariants live here. W4: a release arrives for every T=7 exit including
// dispute, and this service does not look at which one it was before paying.
// W10: every held payment has a reason with an owner, which the schema enforces
// as a constraint rather than a convention.
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
	rail := client.New(config.Str("RAIL_URL", "http://mock-rail:8080"))

	service.Main("payments", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
		Deliver: func(d service.Deps) store.Deliverer {
			return func(ctx context.Context, topic string, payload json.RawMessage) error {
				switch topic {
				case topicRailSend:
					return sendToRail(ctx, d, rail, payload)
				default:
					return fmt.Errorf("no delivery route for topic %q", topic)
				}
			}
		},
	})
}
