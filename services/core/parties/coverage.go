package parties

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

// g4_4, "The headline is not how many are registered" (§3).
//
// The reference frame wants coverage against a place's population — but a
// population figure is deployment data CREST does not own (the layering test:
// two deployments can reasonably disagree about which geography vocabulary
// applies, and both stay CREST). What the registry does own is which parties
// exist and what they self-declared about themselves (schema.Party.Attributes,
// #168) — so this is honest counts-by-place, with a denominator accepted from
// the caller for this one read rather than stored, because storing it would
// make the core the custodian of a number it cannot keep current.
//
// unspecified counts parties whose declared attribute is absent or blank —
// deliberately surfaced rather than dropped, because a place bucket that
// silently excludes the workers with no place on file is exactly the "headline
// is not how many are registered" trap the frame is named for.
const unspecifiedPlace = ""

// placeCount is one bucket of the coverage-by-place read.
type placeCount struct {
	Place      string   `json:"place"`
	Registered int      `json:"registered"`
	Estimate   *int     `json:"estimate,omitempty"`
	Coverage   *float64 `json:"coveragePct,omitempty"`
}

// coverageByPlace groups a set of already-fetched attribute values into
// counts, and joins the caller-supplied denominators (population estimates)
// where one was given for that place. It takes plain strings rather than
// schema.Party so the aggregation itself needs no database to test.
//
// denominators is nil-safe and a place absent from it gets no estimate and no
// percentage: the caller asked a real question ("how many registered here")
// and gets a real answer, rather than a manufactured 0% that reads as "none
// live there".
func coverageByPlace(places []string, denominators map[string]int) []placeCount {
	counts := map[string]int{}
	for _, p := range places {
		counts[strings.TrimSpace(p)]++
	}
	out := make([]placeCount, 0, len(counts))
	for place, n := range counts {
		row := placeCount{Place: place, Registered: n}
		if d, ok := denominators[place]; ok {
			est := d
			row.Estimate = &est
			if d > 0 {
				pct := float64(n) / float64(d) * 100
				row.Coverage = &pct
			}
		}
		out = append(out, row)
	}
	// Deterministic order: unspecified last (it is the residual, not a place),
	// the rest alphabetical so a repeated read is diffable.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Place == unspecifiedPlace {
			return false
		}
		if out[j].Place == unspecifiedPlace {
			return true
		}
		return out[i].Place < out[j].Place
	})
	return out
}

// partyAttributeValues reads one self-declared attribute across every
// non-merged party of a kind, for coverageByPlace to bucket. A missing or
// non-string value comes back as "" — the unspecified bucket — rather than
// being dropped, per coverageByPlace's doc comment.
func partyAttributeValues(ctx context.Context, q store.Querier, kind, attribute string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM parties
		WHERE kind = $1 AND (doc->>'mergedInto') IS NULL`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (string, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return "", err
		}
		var p struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(doc, &p); err != nil {
			return "", err
		}
		v, _ := p.Attributes[attribute].(string)
		return v, nil
	})
}

// coverage is g4_4's endpoint: GET /v1/coverage?attribute=<key>[&denominator.<place>=<n>...]
//
// attribute names which self-declared Party.Attributes key holds the place —
// deployment vocabulary, never assumed by the core (the layering test again).
// denominator.<place> query parameters are this read's only source of
// population figures; omitting them is a legitimate answer, not an error, and
// the response says as much per row via the absent estimate/coveragePct.
func (h *handlers) coverage(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	attribute := strings.TrimSpace(r.URL.Query().Get("attribute"))
	if attribute == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"attribute is required: coverage-by-place has to name which self-declared "+
				"Party attribute holds the place, because the registry has no built-in "+
				"geography vocabulary")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "person"
	}
	denominators := map[string]int{}
	for key, vals := range r.URL.Query() {
		const prefix = "denominator."
		if !strings.HasPrefix(key, prefix) || len(vals) == 0 {
			continue
		}
		place := strings.TrimPrefix(key, prefix)
		n, err := strconv.Atoi(vals[0])
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_parameter",
				"denominator.%s must be an integer population figure", place)
			return
		}
		denominators[place] = n
	}

	values, err := partyAttributeValues(r.Context(), h.d.DB.Q(), kind, attribute)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read party attributes", err)
		return
	}
	byPlace := coverageByPlace(values, denominators)
	total := 0
	for _, row := range byPlace {
		total += row.Registered
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"attribute":       attribute,
		"kind":            kind,
		"totalRegistered": total,
		"byPlace":         byPlace,
		"note": "counts are registered parties grouped by the self-declared attribute " +
			"named in \"attribute\"; a place with no matching denominator query " +
			"parameter carries no estimate or coveragePct, because CREST does not " +
			"own population figures (deployment L2 data)",
	})
}
