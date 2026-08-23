// Command definitions is a CREST service.
//
// Answers "what counts as done, in which version, per which face?" (§13).
//
// Two rules here are infrastructure rather than product, and both are enforced
// in this service rather than left to a UI: a definition is immutable once
// ACTIVE, and the party who authored a version may not be the party who
// ratifies it (§7).
package main

import (
	"embed"

	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	service.Main("definitions", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
	})
}
