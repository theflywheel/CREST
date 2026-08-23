package credential_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/credential"
)

func issuer(t *testing.T) *credential.Issuer {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	iss, err := credential.NewIssuer("did:crest:issuer:fixture", seed)
	if err != nil {
		t.Fatal(err)
	}
	return iss
}

func doc() map[string]any {
	return map[string]any{
		"@context": []any{credential.ContextVC, credential.ContextCREST},
		"id":       "crest:credential:01JCREST00000000000000CRED",
		"type":     []any{"VerifiableCredential", "WorkEventCredential"},
		"issuer":   "did:crest:issuer:fixture",
		"credentialSubject": map[string]any{
			"id": "fixture-psut-anaya",
		},
	}
}

func TestASignedCredentialVerifies(t *testing.T) {
	iss := issuer(t)
	signed, err := iss.Issue(doc(), time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := credential.Verify(signed, iss.PublicKeyMultibase()); err != nil {
		t.Fatalf("a credential we just signed does not verify: %v", err)
	}
}

// The point of a signature: changing what the credential says must break it.
// This is the test that stands behind "verifiable without CREST".
func TestTamperingBreaksTheSignature(t *testing.T) {
	iss := issuer(t)
	signed, _ := iss.Issue(doc(), time.Now())

	subject := signed["credentialSubject"].(map[string]any)
	subject["id"] = "somebody-else"

	if err := credential.Verify(signed, iss.PublicKeyMultibase()); err == nil {
		t.Error("a credential reassigned to a different subject still verified")
	}
}

// Lifting a valid signature onto a proof that claims a different purpose or a
// different key must fail. That is why the proof options are hashed separately.
func TestTheProofOptionsAreCovered(t *testing.T) {
	iss := issuer(t)
	signed, _ := iss.Issue(doc(), time.Now())

	proof := signed["proof"].(map[string]any)
	proof["verificationMethod"] = "did:crest:issuer:somebody-else#key-1"

	if err := credential.Verify(signed, iss.PublicKeyMultibase()); err == nil {
		t.Error("the proof still verified after being re-pointed at another key")
	}
}

func TestAnotherIssuersKeyDoesNotVerify(t *testing.T) {
	iss := issuer(t)
	signed, _ := iss.Issue(doc(), time.Now())

	other := make([]byte, ed25519.SeedSize)
	for i := range other {
		other[i] = byte(255 - i)
	}
	stranger, _ := credential.NewIssuer("did:crest:issuer:stranger", other)

	if err := credential.Verify(signed, stranger.PublicKeyMultibase()); err == nil {
		t.Error("a credential verified under a key that did not sign it")
	}
}

// Canonicalisation is what makes the signature reproducible. If key order
// changed the bytes, a credential would verify or not depending on which JSON
// library the verifier happened to use.
func TestKeyOrderDoesNotChangeTheSignature(t *testing.T) {
	iss := issuer(t)
	at := time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)

	a, _ := iss.Issue(map[string]any{"b": "two", "a": "one", "c": "three"}, at)
	b, _ := iss.Issue(map[string]any{"c": "three", "a": "one", "b": "two"}, at)

	if a["proof"].(map[string]any)["proofValue"] != b["proof"].(map[string]any)["proofValue"] {
		t.Error("the same document written in a different key order signed differently")
	}
}

func TestStatusListRoundTripsAndHidesItsSize(t *testing.T) {
	list := credential.NewStatusList(10)
	if list.Entries() < credential.MinimumEntries {
		t.Errorf("a list of %d entries leaks how many credentials exist", list.Entries())
	}
	if err := list.Revoke(42); err != nil {
		t.Fatal(err)
	}
	encoded, err := list.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := credential.DecodeStatusList(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Revoked(42) {
		t.Error("the revoked bit did not survive the round trip")
	}
	if back.Revoked(43) {
		t.Error("a neighbouring credential came back revoked")
	}
}

func TestStatusListCredentialVerifies(t *testing.T) {
	iss := issuer(t)
	list := credential.NewStatusList(credential.MinimumEntries)
	_ = list.Revoke(7)

	signed, err := iss.StatusListCredential("https://fixture.invalid/status/1", list, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The list is signed for the same reason the credential is: otherwise
	// whoever answers the URL decides who has been revoked.
	if err := credential.Verify(signed, iss.PublicKeyMultibase()); err != nil {
		t.Fatalf("the status list credential does not verify: %v", err)
	}
}
