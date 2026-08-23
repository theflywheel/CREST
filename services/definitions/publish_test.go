package main

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The published face is an allow-list, and this is the test that keeps it one.
//
// A DeDi record is append-only. A field that reaches the node cannot be taken
// back out of it, so the failure mode is not "we published something odd" but
// "we published something permanently". If a new field on Definition should be
// public, add it here deliberately; if this test fails because the projection
// grew, that is the review the append-only log deserves.
func TestPublicFaceIsAnAllowList(t *testing.T) {
	ratifier := "did:crest:party:RATIFIER"
	at := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	def := schema.Definition{
		ID:                "crest:definition:WD-4471",
		Version:           2,
		State:             schema.DefinitionStateACTIVE,
		AuthoredByPartyID: "did:crest:party:AUTHOR",
		RatifiedByPartyID: &ratifier,
		ActivatedAt:       &at,
		CreatedAt:         at,
	}

	got := publicFace(def)
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	want := []string{
		"activatedAt", "activity", "authoredByPartyId", "definitionId", "faces",
		"outcomeUnit", "ratifiedByPartyId", "state", "tierMap", "version",
	}
	if len(keys) != len(want) {
		t.Fatalf("published keys = %v\nwant           %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("published keys = %v\nwant           %v", keys, want)
		}
	}
}

// Separation of duties is a fact a verifier should be able to check for
// themselves, not one they have to accept from the issuer (§7).
func TestPublicFaceCarriesBothGovernanceParties(t *testing.T) {
	ratifier := "did:crest:party:RATIFIER"
	got := publicFace(schema.Definition{
		ID: "d", Version: 1, State: schema.DefinitionStateACTIVE,
		AuthoredByPartyID: "did:crest:party:AUTHOR", RatifiedByPartyID: &ratifier,
	})
	if got["authoredByPartyId"] == got["ratifiedByPartyId"] {
		t.Fatal("author and ratifier are the same in the published face")
	}
	if got["ratifiedByPartyId"] != ratifier {
		t.Errorf("ratifier not published: %v", got["ratifiedByPartyId"])
	}
}

// An unratified definition has no ratifier, and the published face must omit
// the key rather than publish an empty string that reads as "ratified by
// nobody in particular".
func TestPublicFaceOmitsAnAbsentRatifier(t *testing.T) {
	got := publicFace(schema.Definition{ID: "d", Version: 1, AuthoredByPartyID: "a"})
	if _, ok := got["ratifiedByPartyId"]; ok {
		t.Fatal("an absent ratifier was published as a key")
	}
}

// The tier map has to survive the round trip through JSON, because a verifier
// reads it off the node and not out of a Go struct. Trust strength is derived,
// never stored — a verifier who cannot see the map can only be told the tier.
func TestPublicFaceSurvivesJSON(t *testing.T) {
	raw, err := json.Marshal(publicFace(schema.Definition{
		ID: "d", Version: 1, State: schema.DefinitionStateACTIVE, AuthoredByPartyID: "a",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back["tierMap"]; !ok {
		t.Fatal("the tier map did not survive marshalling")
	}
}
