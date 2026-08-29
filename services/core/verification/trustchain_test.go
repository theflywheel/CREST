package verification

import (
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// A Link is only useful if it answers the question it exists to answer. These
// two constructors are the only way to build one precisely so that a link
// cannot be created without deciding whether the verifier can check it.
func TestALinkAlwaysSaysWhereOrWhat(t *testing.T) {
	c := checkable("signed by X", "https://example.org/did.json")
	if !c.Checkable || c.How == "" || c.Trusting != "" {
		t.Errorf("checkable link is malformed: %+v", c)
	}
	a := asserted("signed by X", "this deployment's word")
	if a.Checkable || a.Trusting == "" || a.How != "" {
		t.Errorf("asserted link is malformed: %+v", a)
	}
}

// The authorised-issuer list lives inside the definition record. Claiming a
// verifier can check it independently, when the document it came from is only
// this deployment's copy, is exactly the conflation the Link type exists to
// prevent — so it inherits the definition link's answer rather than declaring
// its own.
func TestIssuerLinkInheritsTheDefinitionsCheckability(t *testing.T) {
	def := schema.Definition{ID: "crest:definition:D", Version: 2}

	got := issuerLink([]Link{checkable("measured under D@2", "https://node/dedi/lookup/…")}, "did:crest:issuer:x", def)
	if !got.Checkable {
		t.Error("the definition resolves independently, so the issuer list inside it does too")
	}

	got = issuerLink([]Link{asserted("measured under D@2", "this deployment's copy")}, "did:crest:issuer:x", def)
	if got.Checkable {
		t.Error("the definition is only this deployment's copy, so the issuer list inside it cannot be checked independently")
	}
	if got.Trusting == "" {
		t.Error("an unverifiable issuer link does not say what is being trusted")
	}
}

// An empty chain cannot be inherited from, and defaulting to "checkable" there
// would be the wrong way to be wrong.
func TestIssuerLinkDefaultsToAsserted(t *testing.T) {
	if issuerLink(nil, "did:crest:issuer:x", schema.Definition{ID: "D", Version: 1}).Checkable {
		t.Fatal("an issuer link with nothing to inherit from claimed to be independently checkable")
	}
}

// The status list URL comes off the credential, so it is the credential's own
// answer rather than this service's — a verifier who fetched a URL this service
// invented would be checking whatever this service pointed them at.
func TestStatusListURLComesFromTheCredential(t *testing.T) {
	doc := map[string]any{"credentialStatus": map[string]any{
		"statusListCredential": "https://issuer.example/v1/status-list",
	}}
	if got := statusListURL(doc); got != "https://issuer.example/v1/status-list" {
		t.Errorf("statusListURL = %q", got)
	}
	// A credential with no status entry still gets a sentence rather than an
	// empty string, because an empty `how` on a checkable link is a link that
	// says nothing.
	if got := statusListURL(map[string]any{}); got == "" {
		t.Error("a credential with no status list produced an empty how")
	}
}
