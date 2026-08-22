// Command registry is a CREST service.
//
// Instances, organisations, terms, authorizations, worker records, duplicate hold queue. Blueprint §3.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("registry", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
