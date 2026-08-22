// Command payments is a CREST service.
//
// Rate resolution, PaymentInstruction emission with idempotency, rail connectors, reconciliation. Blueprint §10.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("payments", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
