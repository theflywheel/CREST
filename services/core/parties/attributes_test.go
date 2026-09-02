package parties

import (
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The organisation profile (#168): kind, sector, country and contact person
// became registry facts via a generic attributes map on Party. L1 holds the
// map; the vocabulary — which keys exist, what values are legal — is
// deployment configuration, per the layering test.

func orgWithAttributes(attrs map[string]any) schema.Party {
	return schema.Party{
		ID:          "did:crest:party:01JCREST00000000000RGATTR0",
		Kind:        schema.PartyKindOrganisation,
		DisplayName: "Lakeside Health Trust",
		CreatedAt:   time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
		ContactRoutes: []schema.PartyContactRoutesItem{
			{Kind: schema.PartyContactRoutesItemKindEmail, Value: "programmes@lakeside.example"},
		},
		Attributes: attrs,
	}
}

func TestAnOrganisationsAttributesAreValidPartyFacts(t *testing.T) {
	p := orgWithAttributes(map[string]any{
		"kind":          "delivery",
		"sector":        "health",
		"country":       "KE",
		"contactPerson": "Hon. Peter Okello",
	})
	if err := schema.Validate(schema.IDParty, p); err != nil {
		t.Fatalf("a party with descriptive attributes must validate: %v", err)
	}
}

func TestAttributeValuesAreBoundedStringsOnly(t *testing.T) {
	// Not a string: the map is for small descriptive facts, not documents.
	p := orgWithAttributes(map[string]any{"sector": 7})
	if err := schema.Validate(schema.IDParty, p); err == nil {
		t.Fatal("a non-string attribute value must be refused")
	}
	// Over-long: a 200-char cap keeps the map from becoming a place to stash
	// blobs — anything that big has a typed record it belongs in.
	p = orgWithAttributes(map[string]any{"sector": strings.Repeat("x", 201)})
	if err := schema.Validate(schema.IDParty, p); err == nil {
		t.Fatal("an over-long attribute value must be refused")
	}
}

// The public face stays an allowlist: self-declared attributes never reach the
// registry log. What an organisation says about itself is applicant-facing
// fact (served on its own registration read), not a published one — publishing
// would let any applicant write arbitrary text into an append-only public log.
func TestOrganisationFaceDoesNotCarryAttributes(t *testing.T) {
	face, err := organisationFace(orgWithAttributes(map[string]any{"sector": "health"}))
	if err != nil {
		t.Fatalf("face: %v", err)
	}
	if _, there := face["attributes"]; there {
		t.Fatal("the public organisation face must not publish self-declared attributes")
	}
}
