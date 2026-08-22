// Command definitions is a CREST service.
//
// Work definitions: author → ratify → ACTIVE, three-face rendering, DeDi publication. Blueprint §7.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("definitions", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
