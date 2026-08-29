package evidence

import (
	"net/http"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/service"
)

// sameParty expands the request's partyId across any merge (#100).
//
// Returns the ids to filter on, and whether the handler should continue. An
// absent partyId is not a filter and expands to nothing, which is the same as
// before merges existed.
//
// A registry that cannot answer stops the read. The tempting alternative —
// fall back to the single id — returns a history with a hole in it and nothing
// saying anything is missing, and the caller believes it. On a system whose
// records decide whether somebody gets paid, a read that fails loudly is worth
// more than one that quietly under-reports.
func sameParty(w http.ResponseWriter, r *http.Request, d service.Deps) ([]string, bool) {
	partyID := r.URL.Query().Get("partyId")
	if partyID == "" {
		return nil, true
	}
	ids, err := d.SameParty(r.Context(), partyID)
	if err != nil {
		d.Log.Error("could not expand a party across merges", "party", partyID, "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "registry_unavailable",
			"the registry could not say which ids are this party, so this list would be "+
				"incomplete without saying so")
		return nil, false
	}
	if len(ids) == 0 {
		return []string{partyID}, true
	}
	return ids, true
}
