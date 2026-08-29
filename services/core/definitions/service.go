// Command definitions is a CREST service.
//
// Answers "what counts as done, in which version, per which face?" (§13).
//
// Two rules here are infrastructure rather than product, and both are enforced
// in this service rather than left to a UI: a definition is immutable once
// ACTIVE, and the party who authored a version may not be the party who
// ratifies it (§7).
package definitions

import (
	"embed"

	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service is this member's wiring, composed into the core binary (#150).
func Service() service.Options {
	return service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
		// Public by design (§3): a work definition's faces say what counts as
		// done and carry no personal data. Nothing else this service holds
		// goes to the node.
		DeDiRegistries: []string{registryName},
		Deliver:        deliver,
	}
}
