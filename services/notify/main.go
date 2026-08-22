// Command notify is a CREST service.
//
// One channel abstraction over SMS, USSD, WhatsApp and push. Internal — not an L1 public API.
//
// Skeleton: serves health only. Endpoints arrive with the issue that owns this
// service — see docs/COMPONENTS.md.
package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/service"
)

func main() {
	service.Main("notify", func(mux *http.ServeMux, d service.Deps) {
		_ = mux
		_ = d
	})
}
