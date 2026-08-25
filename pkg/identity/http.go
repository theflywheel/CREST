package identity

import (
	"log/slog"
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
)

// Authorize is what a handler calls before acting in somebody's name.
//
// `claimed` is the party the request means to act as — from a body field where
// the caller named it, or from the record being acted on where the record
// knows. Either way it is checked against what the caller proved rather than
// believed, and that check is this whole package's reason to exist.
//
// It writes the refusal itself and returns false, so the handler's shape is one
// line and there is no path where a handler forgets to write a response after
// deciding not to act.
func Authorize(w http.ResponseWriter, r *http.Request, log *slog.Logger,
	claimed, contextID string, enforced bool, permits PermitsFunc) (string, bool) {

	caller := From(r.Context())
	party, err := Actor(r.Context(), caller, claimed, contextID, enforced, permits)
	if err == nil {
		return party, true
	}
	if status, code, detail, ok := Denial(err); ok {
		// The refusal is logged with both sides of it. An operator asked "why
		// could this supervisor not confirm for this worker" needs to see the
		// party that was proved next to the party that was named, and neither
		// belongs in the response body.
		log.Info("refused an action in another party's name",
			"path", r.URL.Path, "proved", caller.PartyID, "named", claimed,
			"onBehalfOf", caller.RequestedFor(), "error", err)
		httpx.WriteError(w, status, code, "%s", detail)
		return "", false
	}
	httpx.Fail(w, log, "establish the caller", err)
	return "", false
}

// Authenticated is the lighter gate, for surfaces that any signed-in principal
// may read but an anonymous stranger may not — a custodian queue that shows
// existence rather than content, an operations list. It establishes THAT there
// is a caller, not WHO they are acting as; an endpoint that acts in a party's
// name wants Authorize instead, and using this one there is the #102 mistake
// with a seatbelt on.
func Authenticated(w http.ResponseWriter, r *http.Request, log *slog.Logger, enforced bool) bool {
	if !enforced {
		return true
	}
	caller := From(r.Context())
	if caller.Authenticated() {
		return true
	}
	log.Info("refused an anonymous read of an authenticated surface", "path", r.URL.Path)
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpx.WriteError(w, http.StatusUnauthorized, "no_caller",
		"this surface answers signed-in callers; present a bearer token")
	return false
}
