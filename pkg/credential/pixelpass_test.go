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

// A card their library produced from JSON is CBOR inside, and decoding it
// returns the JSON (#92, fixed). This was the negative test asserting the gap;
// it is now the positive one, against the same vector from their library.
func TestACardFromTheirLibraryHoldingJSONDecodesToTheJSON(t *testing.T) {
	const theirJSONCard = "NCF:PB7EJ4DJ-XIV50BY15H0"

	got, err := credential.DecodePixelPass(theirJSONCard)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("the decoded card is not JSON: %v (got %q)", err, got)
	}
	// {"a":1,"b":"x"} is what their library was handed before it CBOR-wrapped it.
	if doc["a"] != float64(1) || doc["b"] != "x" {
		t.Errorf("decoded %v, want a=1 b=x", doc)
	}
}

// The subset boundary, stated: CBOR that JSON cannot hold is refused with an
// error naming CBOR — never returned as a "successful" decode of bytes nobody
// expected, which was #92's original failure shape.
func TestCBOROutsideTheJSONSubsetIsRefusedByName(t *testing.T) {
	// A map holding a byte string (major 2) — a structure JSON cannot express.
	// Top-level, because detection keys off the document head: a bare byte
	// string's head is also printable ASCII, and a card is a document, not a
	// fragment.
	card, err := credential.EncodePixelPass([]byte{0xa1, 0x61, 'k', 0x42, 0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credential.DecodePixelPass(card); err == nil ||
		!strings.Contains(err.Error(), "CBOR") {
		t.Fatalf("a byte-string payload was not refused with an error naming CBOR: %v", err)
	}
}

// Nested structures, integers that must stay integers, floats, booleans and
// null — hand-written CBOR, decoded and compared as JSON.
func TestTheCBORSubsetCoversWhatJSONCanHold(t *testing.T) {
	// {"n": 9007199254740993, "arr": [1, -2, 2.5, true, null], "s": "worker"}
	// 9007199254740993 = 2^53+1, the first integer a float64 round-trip loses.
	cbor := []byte{
		0xa3, // map(3)
		0x61, 'n', 0x1b, 0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x63, 'a', 'r', 'r', 0x85,
		0x01,
		0x21,
		0xfb, 0x40, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xf5,
		0xf6,
		0x61, 's', 0x66, 'w', 'o', 'r', 'k', 'e', 'r',
	}
	card, err := credential.EncodePixelPass(cbor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := credential.DecodePixelPass(card)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := `{"arr":[1,-2,2.5,true,null],"n":9007199254740993,"s":"worker"}`
	if string(got) != want {
		t.Errorf("decoded %s, want %s", got, want)
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
