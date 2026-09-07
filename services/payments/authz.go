package main

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

// paymentOperationsFunction is deployment vocabulary. A finance authorization
// is always checked against the project context; being signed in or owning a
// worker party is not permission to inspect everybody's payment data.
func paymentOperationsFunction() string {
	// payment-owner is the vocabulary in the published payment terms and is
	// therefore the safe default for a deployment that has not selected a
	// narrower profile-specific name. Deployments with a distinct finance
	// operations role can set PAYMENT_OPERATIONS_FUNCTION explicitly.
	return config.Str("PAYMENT_OPERATIONS_FUNCTION", "payment-owner")
}

func authorizePaymentOperations(w http.ResponseWriter, r *http.Request, d service.Deps, contextID string) bool {
	if contextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"contextId is required for a finance operation")
		return false
	}
	// Finance reads are private even in local mode. Fixtures and an absent
	// provider must never turn an unscoped report into an anonymous export.
	caller := identity.From(r.Context())
	if !caller.Authenticated() {
		httpx.WriteError(w, http.StatusUnauthorized, "no_caller",
			"this surface answers authorized finance callers; present a bearer token")
		return false
	}
	if caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "subject_not_enrolled",
			"the authenticated caller is not enrolled as a party")
		return false
	}
	ok, err := d.Permits(r.Context(), caller.PartyID, paymentOperationsFunction(), contextID)
	if err != nil {
		httpx.Fail(w, d.Log, "check payment operations authorization", err)
		return false
	}
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"the authenticated caller has no finance authorization for this project")
		return false
	}
	return true
}
