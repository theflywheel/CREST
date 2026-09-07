// Command parties is a CREST service.
//
// Answers "who exists, under what terms, on which projects?" (Blueprint §13).
// It also owns the one operation the rest of the system cannot get wrong
// quietly: resolving a source system's joining identifier to a Party. A wrong
// match attributes one person's work to another, and no downstream check
// catches it — so an ambiguous match holds rather than guesses (W4).
package parties

import (
	"context"
	"embed"

	"github.com/theflywheel/crest/pkg/clockctl"
	"github.com/theflywheel/crest/pkg/service"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Service is this member's wiring, composed into the core binary (#150).
func Service() service.Options {
	return service.Options{
		// né "registry" (#50): the word already meant DeDi and the ten public
		// faces, and this service is neither — it is the boundary between the
		// public log and the private store, and Party is its primitive.
		FormerName: "registry",
		Migrations: migrations,
		Dir:        "migrations",
		Routes:     routes,
		// An identity override carries a review date, and a review that has
		// come due has to be findable — "flagged for review" only means
		// something if somebody can find the flag. That is this member's own
		// scheduled behaviour, and it is asserted by moving time past the
		// review date rather than by waiting for it.
		//
		// Declared again after #127 briefly took it away, for the reason
		// written out in evidence's Service(): the ruling that the
		// confirmation window is the payments application's stands, and no
		// window lives here — what was wrong was the corollary that a window
		// is the only reason a process wants driveable time (#215, ruled
		// 2026-09-07). The seam is a non-production harness surface and
		// refuses to start in production, so it carries no programme policy.
		ClockSeam: clockctl.Seam,
		// The registry owns the parties table, so it answers both identity
		// questions locally. Everybody else asks it over HTTP.
		Binder:    localBinder,
		Permits:   localPermits,
		SameParty: localIdentifiers,
		// Consent artefacts: the voice recording that is a non-literate
		// worker's only real way to consent (§9, #24).
		NeedsBlobs: true,
		// The public half of §3, and only the public half. Workers, contact
		// routes and identity bindings have no registry here and never will —
		// what reaches the node is decided field by field in publish.go.
		DeDiRegistries: []string{
			registryOrganisations, registryTerms, registryAuthorizations,
			registryInstances, registrySkills,
		},
		// Publishes the deployment's own self-description, so a verifier who
		// resolves a record on the node can find out which deployment owns the
		// namespace and which publisher key its writes should carry (#70).
		OnStart: func(ctx context.Context, d service.Deps) error {
			if err := publishInstance(ctx, d); err != nil {
				return err
			}
			go uploadRecoveryLoop(ctx, d)
			return nil
		},
		Deliver: deliver,
	}
}
