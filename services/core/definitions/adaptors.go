package definitions

// The adaptor catalogue (p3_24), honestly.
//
// The design reference's library screen shows DHIS2, CommCare, DIGIT HCM and
// more. CREST today has one implemented adapter class: the batch/CSV adapter.
// A DHIS2-shaped source is served by that class plus a per-source column
// mapping (the riverside-dhis2 seed is exactly this), which is what §8
// intends — adapters per system class, configured per deployment. This
// catalogue lists what exists and names what does not, because a library
// screen that pads itself with unimplemented logos is a promise the first
// partner conversation has to walk back.

import (
	"net/http"

	"github.com/theflywheel/crest/adapters/builtin"
	"github.com/theflywheel/crest/pkg/httpx"
)

// AdaptorEntry is one row of the catalogue.
type AdaptorEntry struct {
	Ref          string   `json:"ref"`
	Class        string   `json:"class"`
	Name         string   `json:"name,omitempty"`
	ContentTypes []string `json:"contentTypes,omitempty"`
	Status       string   `json:"status"`
	Note         string   `json:"note,omitempty"`
}

// adaptorCatalogue is built from what the binary actually links, plus named
// absences. Data-driven honesty: the illustrative entries say so.
func adaptorCatalogue() []AdaptorEntry {
	entries := make([]AdaptorEntry, 0)
	for _, connector := range builtin.Registry().Connectors() {
		entries = append(entries, AdaptorEntry{
			Ref: connector.Ref(), Class: connector.Class, Name: connector.Name,
			ContentTypes: connector.ContentTypes, Status: "available", Note: connector.Description,
		})
	}
	return append(entries,
		AdaptorEntry{
			Class:  "dhis2-api",
			Status: "not-implemented",
			Note:   "the reference's library shows this as a class; CREST does not have it yet — DHIS2 sources run through batch-file with a mapping",
		},
		AdaptorEntry{
			Class:  "digit-hcm",
			Status: "not-implemented",
			Note:   "shown in the reference journey (p3_24); not implemented",
		},
	)
}

func (h *handlers) adaptors(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"adaptors": adaptorCatalogue()})
}
