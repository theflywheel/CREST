package credential_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/credential"
)

// One vector produced by MOSIP's own @mosip/pixelpass 0.6.0, and a deliberate
// note about why there is only one.
//
// Byte-for-byte equality with their encoder is the WRONG test, and asserting it
// was the first thing tried here. Two reasons it fails while both outputs are
// perfectly valid: their library CBOR-encodes input that parses as JSON before
// compressing it, and Go's deflate and pako's deflate make different, equally
// legal choices on anything longer than a few dozen bytes.
//
// The property that actually matters is directional. A printed card is read by
// somebody else's scanner, so what must hold is THEIR DECODER READS OUR CARD —
// asserted in `make printed-card`, which feeds a Go-produced payload to their
// library and requires the credential back intact. This vector covers the other
// half: that we read the format they emit.
const theirHelloCard = "NCFKVPV0QSIP600GP5L0"

func TestWeReadTheFormatMosipsLibraryEmits(t *testing.T) {
	got, err := credential.DecodePixelPass(theirHelloCard)
	if err != nil {
		t.Fatalf("decode a card their library produced: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("decoded %q, want %q", got, "hello")
	}
}

// A card their library produced from JSON is CBOR inside, and this decoder does
// not unwrap CBOR — it returns the CBOR bytes rather than the JSON.
//
// Stated as a test rather than left to be discovered. CREST only ever PRINTS
// cards today, so the gap costs nothing yet; it would matter the moment CREST
// has to read a card produced by Inji's wallet, and a silent "successful"
// decode returning bytes nobody expected is the worse failure. Tracked in #92.
func TestACardFromTheirLibraryHoldingJSONComesBackAsCBORNotJSON(t *testing.T) {
	const theirJSONCard = "NCF:PB7EJ4DJ-XIV50BY15H0"

	got, err := credential.DecodePixelPass(theirJSONCard)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if json.Valid(got) {
		t.Fatalf("this decoder now understands CBOR-wrapped cards; #92 is fixed "+
			"and this test should become the positive one: %q", got)
	}
	// 0xa2 is a two-entry CBOR map — {"a":1,"b":"x"} as their library wrote it.
	if len(got) == 0 || got[0] != 0xa2 {
		t.Errorf("expected CBOR bytes, got %q", got)
	}
}

// The card has to survive being a real credential, not a short string: a
// WorkEventCredential is a kilobyte of JSON with a signature in it, and the
// signature is the part where a single altered byte is the whole failure.
func TestARealCredentialRoundTripsThroughACard(t *testing.T) {
	var doc map[string]any
	load(t, "certify-issued-credential.json", &doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	payload, err := credential.EncodePixelPass(raw)
	if err != nil {
		t.Fatal(err)
	}
	back, err := credential.DecodePixelPass(payload)
	if err != nil {
		t.Fatalf("a card built from a real credential did not decode: %v", err)
	}
	if string(back) != string(raw) {
		t.Fatal("the credential came back changed")
	}

	// And it still verifies afterwards, which is the property the printed path
	// exists for. Compression that round-trips but perturbs the document would
	// pass the check above and fail in front of a verifier.
	var decoded map[string]any
	if err := json.Unmarshal(back, &decoded); err != nil {
		t.Fatal(err)
	}
	var did struct {
		VerificationMethod []struct {
			PublicKeyMultibase string `json:"publicKeyMultibase"`
		} `json:"verificationMethod"`
	}
	load(t, "certify-issuer-did.json", &did)
	if err := credential.Verify(decoded, did.VerificationMethod[0].PublicKeyMultibase); err != nil {
		t.Fatalf("the credential no longer verifies after a round trip through a card: %v", err)
	}
}

func TestAPayloadThatIsNotACardIsRefused(t *testing.T) {
	for _, bad := range []string{"", "hello", "NCF!!!", "NCFZZ"} {
		if _, err := credential.DecodePixelPass(bad); err == nil {
			t.Errorf("decoded %q as a card", bad)
		}
	}
	if _, err := credential.DecodePixelPass("NCF" + strings.Repeat("0", 6)); err == nil {
		t.Error("decoded base45 that is not zlib as a card")
	}
}
