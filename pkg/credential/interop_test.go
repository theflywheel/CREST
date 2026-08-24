package credential_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/theflywheel/crest/pkg/credential"
)

func load(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

// The test every other test in this package could not be.
//
// Everything else here signs with this code and verifies with this code, so a
// self-consistent mistake passes all of them. One did: the proof configuration
// was canonicalised without the document's @context, which eddsa-jcs-2022
// requires. The signatures were valid against themselves and against nothing
// else — no conformant verifier could read a CREST credential, and this package
// could not read one issued by Certify.
//
// "Provable to a stranger, offline" is a claim about somebody else's verifier.
// The only way to test it is with bytes we did not produce, so this fixture is
// a real credential issued by the deployed Certify over OpenID4VCI, checked
// against the DID document Certify itself serves. It carries no identifier and
// no tier — asserted below, because a fixture is exactly where that rule gets
// forgotten.
func TestACredentialIssuedByCertifyVerifies(t *testing.T) {
	var doc map[string]any
	load(t, "certify-issued-credential.json", &doc)

	var did struct {
		ID                 string `json:"id"`
		VerificationMethod []struct {
			ID                 string `json:"id"`
			PublicKeyMultibase string `json:"publicKeyMultibase"`
		} `json:"verificationMethod"`
	}
	load(t, "certify-issuer-did.json", &did)

	if doc["issuer"] != did.ID {
		t.Fatalf("fixture mismatch: credential issuer %v, DID document %v", doc["issuer"], did.ID)
	}
	if err := credential.Verify(doc, did.VerificationMethod[0].PublicKeyMultibase); err != nil {
		t.Fatalf("a credential issued by Certify does not verify here: %v.\n"+
			"CREST and the substrate disagree about how the credential is signed, which means "+
			"one of them is producing records the other cannot read.", err)
	}
}

// Tampering with someone else's credential must fail the same way tampering
// with our own does. A verifier that is strict about its own issuer and lax
// about a foreign one is worse than no verifier.
func TestTamperingWithACertifyCredentialIsCaught(t *testing.T) {
	var doc map[string]any
	load(t, "certify-issued-credential.json", &doc)
	var did struct {
		VerificationMethod []struct {
			PublicKeyMultibase string `json:"publicKeyMultibase"`
		} `json:"verificationMethod"`
	}
	load(t, "certify-issuer-did.json", &did)

	subject := doc["credentialSubject"].(map[string]any)
	outcome := subject["outcome"].(map[string]any)
	outcome["value"] = 4200

	if err := credential.Verify(doc, did.VerificationMethod[0].PublicKeyMultibase); err == nil {
		t.Fatal("a credential whose outcome was rewritten still verified")
	}
}

// The rule applies to fixtures too, and this one came off a live issuer.
func TestTheInteropFixtureCarriesNoIdentifierAndNoTier(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "certify-issued-credential.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		`"nationalId"`, `"uin"`, `"individualId"`, `"tier"`, `"trustTier"`, `"face"`, `"biometric"`,
	} {
		if bytesContains(raw, forbidden) {
			t.Errorf("the fixture contains %s", forbidden)
		}
	}
}

func bytesContains(haystack []byte, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(string(haystack), needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
