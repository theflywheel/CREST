// Command verification is a CREST service.
//
// Answers "does it stand, how strongly, on whose authority?" (§13).
//
// It is the one service a stranger talks to, and the one place the strength
// function is applied to a real credential. Two things it deliberately does
// not do: store a tier, or require the credential to be one it issued. A
// verifier that can only check credentials from its own deployment is not a
// verifier, it is a lookup.
package main

import (
	"embed"

	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	service.Main("verification", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
	})
}
