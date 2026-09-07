package definitions

import (
	"net/http"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
)

// These are authorization functions, rather than role names. The parties
// registry remains the authority for their grants; definitions only asks the
// shared permission predicate whether a proven actor may perform an act.
const (
	FunctionDefinitionAuthor   = "work-definition-author"
	FunctionDefinitionApprover = "work-definition-approver"
	FunctionDefinitionSource   = "work-definition-source-owner"
	FunctionRateOwner          = "rate-owner"
)

func contextID(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("contextId"))
}

func contextValue(v string) string { return strings.TrimSpace(v) }

func definitionContext(r *http.Request, def schema.Definition) string {
	if def.ContextID != nil && strings.TrimSpace(*def.ContextID) != "" {
		return strings.TrimSpace(*def.ContextID)
	}
	return ""
}

// authorizeFunction establishes the actor from the verified request and then
// checks the actor's project (or instance, when contextID is empty) grant.
// Caller supplied party ids are cross-checked by identity.Authorize; they are
// never treated as identity evidence by this package.
func authorizeFunction(w http.ResponseWriter, r *http.Request, d service.Deps,
	claimed, ctxID, function string) (string, bool) {
	if d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry is not configured")
		return "", false
	}
	if identity.From(r.Context()).Assisting() {
		httpx.WriteError(w, http.StatusForbidden, "direct_governance_actor_required", "definition governance requires the directly authenticated role holder")
		return "", false
	}
	actor, ok := identity.Authorize(w, r, d.Log, claimed, ctxID, true, d.Permits)
	if !ok {
		return "", false
	}
	permitted, err := d.Permits(r.Context(), actor, function, ctxID)
	if err != nil {
		d.Log.Error("could not check definition authorization", "function", function,
			"context", ctxID, "actor", actor, "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry could not answer this request")
		return "", false
	}
	if !permitted {
		d.Log.Info("refused definition action", "function", function,
			"context", ctxID, "actor", actor)
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"the caller has no authorization for this definition action in this project")
		return "", false
	}
	return actor, true
}

func checkFunction(w http.ResponseWriter, r *http.Request, d service.Deps,
	actor, ctxID, function string) bool {
	if d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry is not configured")
		return false
	}
	ok, err := d.Permits(r.Context(), actor, function, ctxID)
	if err != nil {
		d.Log.Error("could not check definition authorization", "function", function,
			"context", ctxID, "actor", actor, "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry could not answer this request")
		return false
	}
	if !ok {
		d.Log.Info("refused definition action", "function", function,
			"context", ctxID, "actor", actor)
		httpx.WriteError(w, http.StatusForbidden, "not_permitted",
			"the caller has no authorization for this definition action in this project")
		return false
	}
	return true
}

// authorizeDefinitionRead keeps unpublished versions and their governance
// material private while leaving ACTIVE resolution public. An unpublished
// version is readable by its author or by a separately authorized approver.
func authorizeDefinitionRead(w http.ResponseWriter, r *http.Request, d service.Deps,
	def schema.Definition) bool {
	if def.State == schema.DefinitionStateACTIVE || def.State == schema.DefinitionStateSUPERSEDED {
		return true
	}
	if d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry is not configured")
		return false
	}
	ctxID := definitionContext(r, def)
	actor, ok := identity.Authorize(w, r, d.Log, "", ctxID, true, d.Permits)
	if !ok {
		return false
	}
	if actor == def.AuthoredByPartyID {
		return checkFunction(w, r, d, actor, ctxID, FunctionDefinitionAuthor)
	}
	return checkFunction(w, r, d, actor, ctxID, FunctionDefinitionApprover)
}

func authorizeDraft(w http.ResponseWriter, r *http.Request, d service.Deps,
	draft Draft, write bool) bool {
	if d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
			"the authorization registry is not configured")
		return false
	}
	actor, ok := identity.Authorize(w, r, d.Log, "", draft.ContextID, true, d.Permits)
	if !ok {
		return false
	}
	if actor == draft.CreatedBy {
		return checkFunction(w, r, d, actor, draft.ContextID, FunctionDefinitionAuthor)
	}
	if !write && draft.State == draftSubmitted {
		return checkFunction(w, r, d, actor, draft.ContextID, FunctionDefinitionApprover)
	}
	httpx.WriteError(w, http.StatusForbidden, "not_permitted",
		"only the draft author may change or read this draft")
	return false
}
