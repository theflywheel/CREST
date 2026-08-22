// Command evidence is a CREST service.
//
// Adapter intake, canonical record validation, identity matching, unit and claim creation, unclear queue. Blueprint §8.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("evidence", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
