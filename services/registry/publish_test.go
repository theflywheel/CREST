package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

func org() schema.Party {
	return schema.Party{
		ID:          "did:crest:party:ORG",
		Kind:        schema.PartyKindOrganisation,
		DisplayName: "Bednet Distribution Trust",
		CreatedAt:   time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC),
		ContactRoutes: []schema.PartyContactRoutesItem{
			{Kind: schema.PartyContactRoutesItemKindPhone, Value: "+911234567890"},
			{Kind: schema.PartyContactRoutesItemKindEmail, Value: "ops@example.org"},
		},
		IdentityBindings: []schema.PartyIdentityBindingsItem{{
			Provider:   "esignet",
			AssertedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			NationalIDHash: &schema.PartyIdentityBindingsItemNationalIDHash{
				Value: "9f2b7c1e0000000000000000000000000000000000000000000000000000dead",
			},
		}},
	}
}

// A DeDi record is append-only. Whatever reaches it cannot be taken back out,
// so the projection is an allow-list and this is the test that keeps it one.
func TestOrganisationFaceIsAnAllowList(t *testing.T) {
	face, err := organisationFace(org())
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(face))
	for k := range face {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"createdAt", "displayName", "kind", "partyId"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("published keys = %v\nwant           %v", keys, want)
	}
}

// The rule that does not bend: never persist a raw national ID or biometric,
// and a salted hash is still an identity binding that has no business in a
// public append-only log. This asserts on the serialised document, not on the
// map's keys, because a nested field would pass a key check and still ship.
func TestOrganisationFaceCarriesNoContactOrIdentityData(t *testing.T) {
	face, err := organisationFace(org())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(face)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for what, needle := range map[string]string{
		"a phone number":       "+911234567890",
		"an email address":     "ops@example.org",
		"a national-id hash":   "9f2b7c1e",
		"an identity provider": "esignet",
	} {
		if strings.Contains(doc, needle) {
			t.Errorf("%s reached the published document: %s", what, doc)
		}
	}
}

// A person is refused, not filtered. A projection that silently dropped one and
// reported success would leave the caller believing a fact was published when
// nothing was.
func TestOrganisationFaceRefusesAPerson(t *testing.T) {
	p := org()
	p.Kind = schema.PartyKindPerson
	if _, err := organisationFace(p); !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("a person was accepted for publication: err = %v", err)
	}
}

// The design finding in publish.go, as an executable assertion. A worker's
// authorization names a party, a project and a role; a public log of those is a
// permanent roster of who works where. Blueprint §3 says authorizations are
// public without qualifying which, and this is the qualification.
func TestAuthorizationFaceRefusesAWorkersAuthorization(t *testing.T) {
	worker := schema.Party{ID: "did:crest:party:WORKER", Kind: schema.PartyKindPerson}
	auth := schema.Authorization{ID: "crest:authorization:A1", PartyID: worker.ID}
	_, err := authorizationFace(auth, worker)
	if !errors.Is(err, ErrNotPublishable) {
		t.Fatalf("a worker's authorization was accepted for publication: err = %v", err)
	}
}

func TestAuthorizationFacePublishesAnOrganisations(t *testing.T) {
	revoked := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	auth := schema.Authorization{
		ID:               "crest:authorization:A1",
		PartyID:          "did:crest:party:ORG",
		AuthorityPartyID: "did:crest:party:MOH",
		Functions:        []string{"submit-evidence"},
		State:            schema.AuthorizationStateACTIVE,
		RevokedAt:        &revoked,
	}
	face, err := authorizationFace(auth, org())
	if err != nil {
		t.Fatal(err)
	}
	// A revocation that is not published is worse than never having published
	// the grant: a verifier reads an authorization that is no longer real.
	if face["revokedAt"] != "2026-09-01T00:00:00Z" {
		t.Errorf("revokedAt = %v, want it published", face["revokedAt"])
	}
	if face["authorityPartyId"] != "did:crest:party:MOH" {
		t.Errorf("the authority a verifier walks to was not published")
	}
}

func TestTermsFacePublishesTheWholeDocument(t *testing.T) {
	face := termsFace(schema.Terms{
		ID: "crest:terms:T1", Name: "Standard programme terms", Version: 3,
		Permissions: []string{"submit-evidence", "confirm-on-behalf"},
		PublishedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if face["version"] != 3 {
		t.Errorf("version = %v; the version is the fact a verifier walks back to", face["version"])
	}
	perms, ok := face["permissions"].([]string)
	if !ok || len(perms) != 2 {
		t.Errorf("permissions = %v, want both published", face["permissions"])
	}
}

func TestRegistryForRejectsAnUnknownKind(t *testing.T) {
	if _, err := registryFor("worker"); err == nil {
		t.Fatal("'worker' resolved to a registry; workers are never published")
	}
}
