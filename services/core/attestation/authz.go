package attestation

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

func authorizeWindowDetails(w http.ResponseWriter, r *http.Request, d service.Deps, win Window) bool {
	caller := identity.From(r.Context())
	if caller.Authenticated() && !caller.Assisting() && caller.PartyID == win.PartyID {
		return true
	}
	if caller.Assisting() {
		_, ok := identity.Authorize(w, r, d.Log, win.PartyID, win.ContextID,
			d.Authenticating, d.Permits)
		return ok
	}
	return authorizeReviewOperations(w, r, d, win.ContextID)
}

// reviewOperationsFunction is deployment vocabulary. A review authorization
// is always checked against the project context; being signed in or owning a
// worker party is not permission to inspect everybody's payment data.
func reviewOperationsFunction() string {
	return config.Str("REVIEW_OPERATIONS_FUNCTION", "claim-review")
}

func authorizeReviewOperations(w http.ResponseWriter, r *http.Request, d service.Deps, contextID string) bool {
	if contextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"contextId is required for a review operation")
		return false
	}
	// Finance reads are private even in local mode. Fixtures and an absent
	// provider must never turn an unscoped report into an anonymous export.
	caller := identity.From(r.Context())
	if !caller.Authenticated() {
		httpx.WriteError(w, http.StatusUnauthorized, "no_caller",
			"this surface answers authorized review callers; present a bearer token")
		return false
	}
	if caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "subject_not_enrolled",
			"the authenticated caller is not enrolled as a party")
		return false
	}
	ok, err := d.Permits(r.Context(), caller.PartyID, reviewOperationsFunction(), contextID)
	if err != nil {
		httpx.Fail(w, d.Log, "check payment operations authorization", err)
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"the authenticated caller has no review authorization for this project")
		return false
	}
	return true
}
