// Package builtin is the one registration point for the adapters compiled
// into CREST. Every service that parses evidence composes its registry from
// this list, so adding an adapter package is one entry here and nowhere else.
package builtin

import (
	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/adapters/csv"
)

// Plugins lists the statically linked adapter versions this build ships.
// The registry validates them at start-up: a duplicate, unversioned or
// mis-declared entry stops the service rather than parsing evidence wrongly.
func Plugins() []adapters.Plugin {
	return []adapters.Plugin{
		csv.Plugin(),
	}
}
