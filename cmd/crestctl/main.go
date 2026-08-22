// Command crestctl is the operator and harness CLI.
//
// The harness must drive CREST through real interfaces (docs/TESTING.md), and
// this is one of them: seed a fixture world, ratify a definition, advance the
// clock, drive a flow. Anything crestctl can do, an operator can do — which is
// also how we find out whether an operation is actually usable.
//
// Subcommands arrive with the services they drive.
package main

import (
	"fmt"
	"os"
)

const usage = `crestctl — CREST operator and harness CLI

Usage:
  crestctl <command> [flags]

Commands:
  version          Print the version
  help             Show this message

Planned (arrive with the services they drive):
  world seed       Seed the canonical fixture world (#40)
  definition       Author, ratify and publish work definitions (#21)
  evidence submit  Push a CSV batch through the adapter (#22)
  clock advance    Move the injected clock — how a 7-day window is tested in ms
`

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "crestctl: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
