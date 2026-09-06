package parties

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

// FunctionResolveUnclearEvidence is the deployment-level custodian function.
// It covers identity holds and is checked against the hold's project scope.
const FunctionResolveUnclearEvidence = "resolve-unclear-evidence"

// actualCaller is intentionally separate from identity.Authorize's returned
// acting party. Audit fields must name the authenticated principal, not a party
// selected through an on-behalf-of header.
func actualCaller(r *http.Request) (string, bool) {
	c := identity.From(r.Context())
	return c.PartyID, c.Authenticated() && c.PartyID != ""
}

// requireRegistryCustodian requires a directly authenticated, assigned
// custodian. This role is non-delegable: a project-scoped assignment is the
// boundary that prevents an arbitrary signed-in worker from reading or closing
// the registry's sensitive queues.
func requireRegistryCustodian(w http.ResponseWriter, r *http.Request, d service.Deps, contextID string) (string, bool) {
	if !identity.Authenticated(w, r, d.Log, d.Authenticating) {
		return "", false
	}
	c := identity.From(r.Context())
	if c.Assisting() || c.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "custodian_required",
			"this registry operation must be performed by the assigned custodian directly")
		return "", false
	}
	if d.Permits == nil {
		httpx.WriteError(w, http.StatusForbidden, "custodian_not_assigned",
			"the caller has no registry-custodian assignment")
		return "", false
	}
	ok, err := d.Permits(r.Context(), c.PartyID, FunctionResolveUnclearEvidence, contextID)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the custodian assignment could not be checked")
		return "", false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "custodian_not_assigned",
			"the caller is not assigned to resolve this registry scope")
		return "", false
	}
	return c.PartyID, true
}
