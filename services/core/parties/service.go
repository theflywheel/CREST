// Command parties is a CREST service.
//
// Answers "who exists, under what terms, on which projects?" (Blueprint §13).
// It also owns the one operation the rest of the system cannot get wrong
// quietly: resolving a source system's joining identifier to a Party. A wrong
// match attributes one person's work to another, and no downstream check
// catches it — so an ambiguous match holds rather than guesses (W7).
package parties

import (
	"embed"

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
		OnStart: publishInstance,
		Deliver: deliver,
	}
}
