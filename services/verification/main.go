// Command verification is a CREST service.
//
// Verifier passes, trust-chain walk, strength derivation, per-request disclosure consent. Blueprint §6, §9.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("verification", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
