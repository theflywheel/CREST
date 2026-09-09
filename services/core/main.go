// Command core is CREST's one infrastructure service (#150).
//
// With DeDi holding registries, eSignet holding login and Inji holding
// credentials, what CREST's own infrastructure code amounts to is
// orchestration — and one process can say so. The four members keep their
// schemas, migrations, outboxes and route families; what merged is the
// deployment shape, not the boundaries. The payments application (#127,
// #129) deliberately stays outside — including the confirmation window,
// which moved out of this process with its attestation member (#127).
//
// Nothing here reads CONFIRMATION_WINDOW, and nothing here holds a window.
// This process does have scheduled behaviour of its own — evidence's
// source-quiet monitor, parties' override review date — and each of those is a
// configured duration read at start-up rather than a clock anybody can move
// (ruled 2026-09-09).
package main

import (
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/services/core/definitions"
	"github.com/theflywheel/crest/services/core/evidence"
	"github.com/theflywheel/crest/services/core/parties"
	"github.com/theflywheel/crest/services/core/verification"
)

func main() {
	service.Compose("core", []service.Member{
		// parties first: it is the identity authority, and Compose takes the
		// binder, permits and same-party answers for the whole process from
		// the one member that defines them.
		{Name: "parties", Opts: parties.Service()},
		{Name: "definitions", Opts: definitions.Service()},
		{Name: "evidence", Opts: evidence.Service()},
		{Name: "verification", Opts: verification.Service()},
	})
}
