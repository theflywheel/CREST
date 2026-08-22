// Command confirmation is a CREST service.
//
// The T=7 state machine: notify, confirm, dispute, auto-confirm, then issuance. Blueprint §11.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("confirmation", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
