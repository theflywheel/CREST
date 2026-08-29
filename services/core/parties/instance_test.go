package parties

import (
	"errors"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/dedi"
)

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// A deployment that has not been told who it is must say so, and say which
// variables are missing. The alternative — inventing an identifier — publishes
// under a name nobody agreed to, permanently.
func TestLoadInstanceRefusesToInventAnIdentity(t *testing.T) {
	withEnv(t, map[string]string{
		"CREST_INSTANCE_ID": "", "CREST_INSTANCE_NAME": "", "CREST_OPERATOR_PARTY_ID": "",
	})
	_, err := loadInstance(config.Base{Env: "production"})
	if !errors.Is(err, ErrNoInstance) {
		t.Fatalf("a production deployment with no identity started anyway: %v", err)
	}
	// The error names every missing variable at once. One at a time is three
	// deploys to find out.
	for _, want := range []string{"CREST_INSTANCE_ID", "CREST_INSTANCE_NAME", "CREST_OPERATOR_PARTY_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

// Two deployments that both defaulted would collide in one namespace, so the
// convenience default exists only where there is one deployment by definition.
func TestLoadInstanceDefaultsOnlyLocally(t *testing.T) {
	withEnv(t, map[string]string{
		"CREST_INSTANCE_ID": "", "CREST_INSTANCE_NAME": "",
		"CREST_OPERATOR_PARTY_ID": "did:crest:party:ORG",
	})
	inst, err := loadInstance(config.Base{Env: "local"})
	if err != nil {
		t.Fatalf("local development needs no ceremony to start: %v", err)
	}
	if inst.ID == "" || inst.Name == "" {
		t.Fatalf("local defaults did not apply: %+v", inst)
	}
	// The operator is required even locally. It is the one field nobody can
	// guess on someone's behalf.
	t.Setenv("CREST_OPERATOR_PARTY_ID", "")
	if _, err := loadInstance(config.Base{Env: "local"}); !errors.Is(err, ErrNoInstance) {
		t.Error("a local deployment defaulted its own operator")
	}
}

// The published face is what a verifier reads. It must carry the key id they
// should expect on the namespace's records, or resolving the instance tells
// them nothing they can act on.
func TestInstanceFaceCarriesWhatAVerifierNeeds(t *testing.T) {
	face := instanceFace(Instance{
		ID: "crest:instance:kenya", Name: "Kenya", OperatorPartyID: "did:crest:party:MOH",
		IssuerID: "did:web:crest.example:v1:certify",
		Registry: InstanceRegistry{
			URL: "https://node.example", Namespace: "crest",
			PublisherKeyID: "crest-services", Transparent: true,
		},
	})
	reg, ok := face["registry"].(map[string]any)
	if !ok {
		t.Fatal("no registry block in the published face")
	}
	if reg["publisherKeyId"] != "crest-services" {
		t.Errorf("publisherKeyId = %v; without it a reader cannot tell whose writes these are", reg["publisherKeyId"])
	}
	if face["operatorPartyId"] != "did:crest:party:MOH" {
		t.Errorf("operatorPartyId = %v; 'who operates this' is the point", face["operatorPartyId"])
	}
	// The node's own URL is deliberately absent from the published document.
	// A reader holding this record already reached the node to get it, and a
	// self-reported address on an append-only log is a redirect nobody can
	// withdraw.
	if _, found := reg["url"]; found {
		t.Error("the published face carries the node's own URL")
	}
}

// Bootstrap runs on every start, and the rule that keeps that from filling the
// log with duplicates is content-addressing, not existence.
//
// "Publish if absent" would freeze the first answer forever: a deployment that
// changed operator or rotated its publisher key would go on advertising the old
// one, and the log — the thing a verifier trusts — would become the most
// confidently wrong copy in the system.
func TestAnUnchangedIdentityHashesTheSame(t *testing.T) {
	inst := Instance{
		ID: "crest:instance:kenya", Name: "Kenya", OperatorPartyID: "did:crest:party:MOH",
		Registry: InstanceRegistry{Namespace: "crest", PublisherKeyID: "crest-services", Transparent: true},
	}
	first, err := dedi.Digest(instanceFace(inst))
	if err != nil {
		t.Fatal(err)
	}
	again, err := dedi.Digest(instanceFace(inst))
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatalf("the same identity hashed two ways: %s then %s\n"+
			"a restart would republish it, and the log would stop being a history", first, again)
	}

	// A rotated publisher key is exactly the change that must reach the log:
	// a verifier checking who signed a record against a stale key id would
	// reject writes that are genuinely ours.
	rotated := inst
	rotated.Registry.PublisherKeyID = "crest-services-2"
	changed, err := dedi.Digest(instanceFace(rotated))
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("rotating the publisher key did not change the digest, so it would never be republished")
	}
}
