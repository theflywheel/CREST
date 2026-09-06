package verification

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

// authorizeParty is the mandatory gate for private credential and audit reads.
// It deliberately ignores the deployment's local auth toggle: a private
// handler must have a verified caller even in a single-service development run.
func authorizeParty(w http.ResponseWriter, r *http.Request, d service.Deps, partyID string) bool {
	if !identity.Authenticated(w, r, d.Log, true) {
		return false
	}
	_, ok := identity.Authorize(w, r, d.Log, partyID, "", true, d.Permits)
	return ok
}
