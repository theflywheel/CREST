package harness

import (
	"net/http/httptest"
	"testing"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

func TestPhoneOfAcceptsRegistryRuntimePartyIDs(t *testing.T) {
	const fixtureID = "did:crest:party:fixture-worker"
	const runtimeID = "did:crest:party:runtime-worker"
	w := &fixtures.World{
		Parties: []schema.Party{{
			ID: fixtureID,
			ContactRoutes: []schema.PartyContactRoutesItem{{
				Kind:  schema.PartyContactRoutesItemKindPhone,
				Value: "+15550100011",
			}},
		}},
		RuntimeIDs: map[string]string{fixtureID: runtimeID},
	}
	phone, err := PhoneOf(w, runtimeID)
	if err != nil {
		t.Fatalf("PhoneOf(runtime id): %v", err)
	}
	if phone != "+15550100011" {
		t.Fatalf("phone = %q", phone)
	}
}

func TestCallerCarriesIdempotencyKey(t *testing.T) {
	service := New().Parties.As(Caller{Token: "token", IdempotencyKey: "operation-key"})
	req := httptest.NewRequest("POST", "/v1/mutation", nil)
	service.apply(req)
	if got := req.Header.Get("Idempotency-Key"); got != "operation-key" {
		t.Fatalf("Idempotency-Key = %q", got)
	}
}
