// Package builtin is the single registration point for connectors compiled
// into the CREST service.
package builtin

import (
	"github.com/theflywheel/crest/adapters"
	csvadapter "github.com/theflywheel/crest/adapters/csv"
)

// Registry returns all connector implementations shipped with CREST.
// Adding a connector package requires one registration entry here.
func Registry() *adapters.Registry {
	return adapters.MustRegistry(
		adapters.Connector{
			Adapter:      csvadapter.Adapter{},
			Class:        "batch-file",
			Name:         "Delimited file",
			ContentTypes: []string{"text/csv", "application/csv", "text/plain"},
			Description:  "CSV or delimited exports with per-source field mapping",
		},
	)
}
