package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

// The registry's half of caller identity (#89).
//
// Two things live here, and they are the two the other six services cannot do
// for themselves: turning a verified subject into a Party, and answering the
// authorization question without a network hop. Both are one SQL query away
// here and an HTTP round trip away everywhere else, which is the whole reason
// pkg/service takes them as options.

// registerIdentityRoutes adds the subject lookup.
//
// Under /internal/ because the middleware does not authenticate that prefix and
// must not: a service resolving who the caller is cannot itself require a
// resolved caller. identity.PathSubjectBinding says what that costs and whose
// job it is to close.
func registerIdentityRoutes(mux *http.ServeMux, d service.Deps) {
	mux.HandleFunc("GET "+identity.PathSubjectBinding+"{subject}", func(w http.ResponseWriter, r *http.Request) {
		subject := strings.TrimSpace(r.PathValue("subject"))
		if decoded, err := url.PathUnescape(subject); err == nil {
			subject = decoded
		}
		if subject == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "no subject")
			return
		}
		party, err := partyForSubject(r.Context(), d.DB.Q(), subject)
		if err != nil {
			httpx.Fail(w, d.Log, "resolve subject", err)
			return
		}
		if party == "" {
			// 404 rather than a null party id. The caller — the middleware in
			// another service — treats it as "authenticated, not enrolled
			// here", which is a state to be enrolled out of rather than an
			// error to retry.
			httpx.WriteError(w, http.StatusNotFound, "not_found", "no party is bound to that subject")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"partyId": party})
	})
}

// registerMergeRoutes exposes which party ids are one person (#100).
//
// Two doors to the same answer, because #104 ruled that the answer is not for
// everyone. The merge is the worker's own history — "where is the rest of my
// record" — and the services' plumbing; it is NOT a verifier's business, since
// "this person was recorded twice" is a fact about the worker they never
// volunteered, and a verifier gets the chain's credentials without the chain
// (see verification). So:
//
//   - /internal/... is for the other services (remoteSameParty), unauthenticated
//     for the same reason the subject-binding route is: restricting /internal/
//     to the service network is the deployment's job, stated in remote.go.
//   - /v1/... answers only the worker, or somebody authorised to act for them.
func registerMergeRoutes(mux *http.ServeMux, d service.Deps) {
	answer := func(w http.ResponseWriter, r *http.Request) {
		ids, err := identifiersOf(r.Context(), d.DB.Q(), r.PathValue("id"))
		if err != nil {
			httpx.Fail(w, d.Log, "resolve party identifiers", err)
			return
		}
		if len(ids) == 0 {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "no such party")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"partyId":     ids[0],
			"identifiers": ids,
			"merged":      len(ids) > 1,
		})
	}
	mux.HandleFunc("GET /internal/parties/{id}/identifiers", answer)
	mux.HandleFunc("GET /v1/parties/{id}/identifiers", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := identity.Authorize(w, r, d.Log, r.PathValue("id"), "",
			d.Authenticating, d.Permits); !ok {
			return
		}
		answer(w, r)
	})
}

// localIdentifiers answers the same question without a network hop.
func localIdentifiers(d service.Deps) service.SamePartyFunc {
	return func(ctx context.Context, partyID string) ([]string, error) {
		return identifiersOf(ctx, d.DB.Q(), partyID)
	}
}

// localBinder resolves subjects from the registry's own table.
func localBinder(d service.Deps) identity.Binder {
	return identity.BinderFunc(func(ctx context.Context, subject string) (string, error) {
		return partyForSubject(ctx, d.DB.Q(), subject)
	})
}

// localPermits answers the authorization question without asking itself over
// HTTP, which would deadlock a single-threaded moment and is absurd regardless.
func localPermits(d service.Deps) identity.PermitsFunc {
	return func(ctx context.Context, partyID, function, contextID string) (bool, error) {
		ok, _, err := permits(ctx, d.DB.Q(), partyID, function, contextID, d.Clock.Now())
		return ok, err
	}
}
