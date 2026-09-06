package harness

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

func TestRuntimeIDsResolveRequestsWithoutRewritingSignedMaterial(t *testing.T) {
	s := New()
	s.SetRuntimeID(fixtures.WorkerAID, "did:crest:party:runtime-worker")

	path := s.resolveRuntimeText("/v1/parties/" + fixtures.WorkerAID + "/credentials")
	if want := "/v1/parties/did:crest:party:runtime-worker/credentials"; path != want {
		t.Fatalf("resolved path = %q, want %q", path, want)
	}
	raw := []byte(`{"partyId":"did:crest:party:01JCREST00000000000000WRKA","credential":{"subject":"did:crest:party:01JCREST00000000000000WRKA"}}`)
	rewritten := rewriteJSONIDs(raw, s.resolveRuntimeText)
	var got map[string]any
	if err := json.Unmarshal(rewritten, &got); err != nil {
		t.Fatal(err)
	}
	if got["partyId"] != "did:crest:party:runtime-worker" {
		t.Fatalf("party id was not resolved: %v", got["partyId"])
	}
	credential := got["credential"].(map[string]any)
	if credential["subject"] != fixtures.WorkerAID {
		t.Fatalf("signed credential material was rewritten: %v", credential["subject"])
	}
}

func TestRuntimeIDsLeaveSignedListsAndExactNumbersIntact(t *testing.T) {
	s := New()
	s.SetRuntimeID(fixtures.WorkerAID, "did:crest:party:runtime-worker")
	raw := []byte(`{"counter":9007199254740993,"credentials":[{"credentialSubject":{"id":"did:crest:party:01JCREST00000000000000WRKA"},"proof":{"proofValue":"signed"}}]}`)
	got := rewriteJSONIDs(raw, s.resolveRuntimeText)
	if !bytes.Contains(got, []byte(`9007199254740993`)) || !bytes.Contains(got, []byte(fixtures.WorkerAID)) {
		t.Fatalf("rewriting changed signed material or an exact integer: %s", got)
	}
	svc := s.Parties.As(Caller{Token: "user-token", OnBehalfOf: fixtures.WorkerAID})
	req := httptest.NewRequest("GET", "/v1/parties/worker", nil)
	svc.apply(req)
	if req.Header.Get("X-CREST-On-Behalf-Of") != "did:crest:party:runtime-worker" || req.Header.Get("Authorization") != "Bearer user-token" {
		t.Fatal("assisted request did not preserve the caller and resolve the target")
	}
}

func TestVerifyRuntimePartyUsesPersistedIDDespiteResponseAliases(t *testing.T) {
	const runtimeID = "did:crest:party:runtime-worker"
	s := New()
	s.SetRuntimeID(fixtures.WorkerAID, runtimeID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/parties/"+runtimeID {
			t.Fatalf("party lookup path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(schema.Party{ID: runtimeID, DisplayName: "Worker Amina"})
	}))
	defer server.Close()
	s.Parties.Base = server.URL

	if err := verifyRuntimeParty(t.Context(), s.Parties, runtimeID, "Worker Amina"); err != nil {
		t.Fatalf("verifyRuntimeParty rejected the persisted runtime id: %v", err)
	}
}

func TestEveryServiceClientResolvesRuntimeIDs(t *testing.T) {
	s := New()
	s.SetRuntimeID(fixtures.WorkerAID, "did:crest:party:runtime-worker")
	for _, svc := range []*Service{s.Parties, s.Definitions, s.Evidence, s.Confirmation, s.Verification, s.Payments, s.Rail} {
		if svc.resolve == nil || svc.reverse == nil {
			t.Fatalf("%s has no runtime ID mapping", svc.Name)
		}
		if got := svc.resolve(fixtures.WorkerAID); got != "did:crest:party:runtime-worker" {
			t.Errorf("%s resolves worker to %q", svc.Name, got)
		}
	}
}
