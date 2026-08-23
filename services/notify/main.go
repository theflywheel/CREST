// Command notify is a CREST service.
//
// It reaches workers. That is the whole job, and it is load-bearing: W2 says a
// worker sees what was recorded about them before it counts, and a confirmation
// window nobody was told about is a window that only ever exits by
// auto-confirm.
//
// The channel is a mock in local and harness runs. Deliberately: a real SMS
// gateway would make the harness cost money and depend on someone else's
// uptime, and neither of those improves the thing being tested.
package main

import (
	"embed"

	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	service.Main("notify", service.Options{
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
	})
}
