package credential

// CBOR unwrapping for PixelPass cards (#92).
//
// MOSIP's @mosip/pixelpass CBOR-encodes any input that parses as JSON before
// compressing it, so every card their library produces from a verifiable
// credential is CBOR inside. Until this file, DecodePixelPass handed back the
// raw CBOR bytes as a "successful" decode — the bad kind of failure, since a
// caller's json.Unmarshal then failed with an error naming nothing about CBOR.
//
// This is deliberately a subset decoder, not a CBOR library. A JSON document
// can only ever contain maps, arrays, strings, numbers, booleans and null, so
// that is all their encoder can emit and all this accepts. Anything outside
// the subset — tags, byte strings, indefinite lengths — is refused with an
// error that names CBOR, because a card carrying it was not produced from a
// JSON credential and pretending otherwise would be the same silence this file
// exists to remove. Tested against vectors from their library, the same way
// base45 is: a codec agreeing with itself proves nothing (the C19 lesson).

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// ErrCBOR marks a payload that is CBOR but not the JSON-compatible subset a
// PixelPass card can legitimately hold.
var ErrCBOR = errors.New("CBOR the JSON subset cannot hold")

// looksLikeCBOR reports whether decompressed card bytes are CBOR rather than
// JSON text. JSON begins with '{', '[', '"', a digit, '-', 't', 'f', 'n' or
// whitespace — all below 0x80 — while a CBOR document encoding a JSON value
// begins with an array (0x80–0x9f) or map (0xa0–0xbf) head, or a float/simple
// (0xe0+). The overlap region (a bare CBOR text string, 0x60–0x7f) is
// unreachable from their encoder's input, which is always a JSON document.
func looksLikeCBOR(b []byte) bool {
	return len(b) > 0 && b[0] >= 0x80
}

// cborToJSON decodes the subset and re-serialises as JSON. Key order is not
// preserved — it cannot be through a Go map — and that is fine for the one
// consumer this exists for: signature verification canonicalises with JCS,
// which sorts keys itself.
func cborToJSON(b []byte) ([]byte, error) {
	v, rest, err := cborDecode(b)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the document", ErrCBOR, len(rest))
	}
	return json.Marshal(v)
}

func cborDecode(b []byte) (any, []byte, error) {
	if len(b) == 0 {
		return nil, nil, fmt.Errorf("%w: truncated", ErrCBOR)
	}
	major, info := b[0]>>5, b[0]&0x1f
	rest := b[1:]

	// The additional-information field: a small count inline, or the size of
	// the count that follows. 31 is the indefinite-length marker, which their
	// encoder never emits and this decoder refuses.
	var n uint64
	switch {
	case info < 24:
		n = uint64(info)
	case info == 24, info == 25, info == 26, info == 27:
		size := 1 << (info - 24)
		if len(rest) < size {
			return nil, nil, fmt.Errorf("%w: truncated length", ErrCBOR)
		}
		switch size {
		case 1:
			n = uint64(rest[0])
		case 2:
			n = uint64(binary.BigEndian.Uint16(rest))
		case 4:
			n = uint64(binary.BigEndian.Uint32(rest))
		case 8:
			n = binary.BigEndian.Uint64(rest)
		}
		rest = rest[size:]
	default:
		return nil, nil, fmt.Errorf("%w: indefinite length or reserved info %d", ErrCBOR, info)
	}

	switch major {
	case 0: // unsigned int
		if n > math.MaxInt64 {
			return nil, nil, fmt.Errorf("%w: integer above int64", ErrCBOR)
		}
		// Kept as an integer: a large int64 does not survive a float64
		// round-trip, and a quantity is exactly the field where that matters.
		return int64(n), rest, nil
	case 1: // negative int, encoded as -1 - n
		if n > math.MaxInt64-1 {
			return nil, nil, fmt.Errorf("%w: integer below int64", ErrCBOR)
		}
		return -1 - int64(n), rest, nil
	case 3: // text string
		if uint64(len(rest)) < n {
			return nil, nil, fmt.Errorf("%w: truncated string", ErrCBOR)
		}
		s := rest[:n]
		if !utf8.Valid(s) {
			return nil, nil, fmt.Errorf("%w: text string is not UTF-8", ErrCBOR)
		}
		return string(s), rest[n:], nil
	case 4: // array
		out := make([]any, 0, min64(n, 1024))
		for i := uint64(0); i < n; i++ {
			var v any
			var err error
			v, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, v)
		}
		return out, rest, nil
	case 5: // map — keys must be text, because JSON's are
		out := make(map[string]any, min64(n, 1024))
		for i := uint64(0); i < n; i++ {
			var k, v any
			var err error
			k, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, nil, fmt.Errorf("%w: a map key that is not text", ErrCBOR)
			}
			v, rest, err = cborDecode(rest)
			if err != nil {
				return nil, nil, err
			}
			out[key] = v
		}
		return out, rest, nil
	case 7: // simple values and floats
		switch info {
		case 20:
			return false, rest, nil
		case 21:
			return true, rest, nil
		case 22:
			return nil, rest, nil
		case 25:
			return finiteFloat(float64(halfToFloat(uint16(n))), rest)
		case 26:
			return finiteFloat(float64(math.Float32frombits(uint32(n))), rest)
		case 27:
			return finiteFloat(math.Float64frombits(n), rest)
		}
		return nil, nil, fmt.Errorf("%w: simple value %d", ErrCBOR, info)
	case 2:
		return nil, nil, fmt.Errorf("%w: a byte string", ErrCBOR)
	default: // 6: tags
		return nil, nil, fmt.Errorf("%w: tag %d", ErrCBOR, n)
	}
}

// finiteFloat admits a decoded float only if JSON could carry it back out.
// CBOR can spell NaN and the infinities; JSON cannot, so a card holding one
// was not produced from a JSON credential and is refused here by name rather
// than surfacing later as an opaque json.Marshal error.
func finiteFloat(f float64, rest []byte) (any, []byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, nil, fmt.Errorf("%w: a non-finite float", ErrCBOR)
	}
	return f, rest, nil
}

// halfToFloat expands IEEE 754 binary16, which CBOR uses for small floats.
func halfToFloat(h uint16) float32 {
	sign := float32(1)
	if h&0x8000 != 0 {
		sign = -1
	}
	exp := int(h>>10) & 0x1f
	frac := float32(h & 0x3ff)
	switch exp {
	case 0: // subnormal
		return sign * frac / 1024 * float32(math.Exp2(-14))
	case 31:
		if frac == 0 {
			return sign * float32(math.Inf(1))
		}
		return float32(math.NaN())
	}
	return sign * (1 + frac/1024) * float32(math.Exp2(float64(exp-15)))
}

func min64(n, limit uint64) uint64 {
	if n < limit {
		return n
	}
	return limit
}
