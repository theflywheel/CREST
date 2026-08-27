package credential

// Internal rather than in pixelpass_test.go: NaN cannot be reached through
// EncodePixelPass, whose input is JSON — the whole point is that JSON has no
// spelling for it — so the vectors here are raw CBOR fed to the decoder.

import (
	"errors"
	"testing"
)

// CBOR can spell NaN and the infinities; JSON cannot. A card carrying one was
// not produced from a JSON credential, and the decoder promises to say so by
// name rather than letting json.Marshal fail opaquely downstream.
func TestNonFiniteFloatsAreRefusedAsOutsideTheJSONSubset(t *testing.T) {
	for name, b := range map[string][]byte{
		"half NaN":            {0xf9, 0x7e, 0x00},
		"half +Infinity":      {0xf9, 0x7c, 0x00},
		"half -Infinity":      {0xf9, 0xfc, 0x00},
		"single NaN":          {0xfa, 0x7f, 0xc0, 0x00, 0x00},
		"double NaN":          {0xfb, 0x7f, 0xf8, 0, 0, 0, 0, 0, 0},
		"double Infinity":     {0xfb, 0x7f, 0xf0, 0, 0, 0, 0, 0, 0},
		"NaN inside an array": {0x81, 0xf9, 0x7e, 0x00}, // [NaN]
	} {
		if _, err := cborToJSON(b); !errors.Is(err, ErrCBOR) {
			t.Errorf("%s: err = %v, want ErrCBOR naming the subset", name, err)
		}
	}
}

// The guard refuses only what JSON cannot hold; an ordinary float still passes.
func TestAFiniteFloatStillDecodes(t *testing.T) {
	out, err := cborToJSON([]byte{0xf9, 0x3c, 0x00}) // 1.0 as binary16
	if err != nil {
		t.Fatalf("a finite half float was refused: %v", err)
	}
	if string(out) != "1" {
		t.Errorf("decoded %q, want 1", out)
	}
}
