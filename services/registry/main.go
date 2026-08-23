// Command registry is a CREST service.
//
// Answers "who exists, under what terms, on which projects?" (Blueprint §13).
// It also owns the one operation the rest of the system cannot get wrong
// quietly: resolving a source system's joining identifier to a Party. A wrong
// match attributes one person's work to another, and no downstream check
// catches it — so an ambiguous match holds rather than guesses (W7).
package main

import (
	"embed"

	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	service.Main("registry", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
	})
}
