package credential

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strings"
)

// PixelPass: the printed card's payload (Blueprint §5, #24).
//
// A printed card carries the full signed credential, not a link to one. That is
// the whole point of the offline path — a bare scan verifies signature and
// schema with no network, so a worker's record is provable to a stranger who
// has never heard of CREST and has no signal. A QR holding a URL would move the
// proof back onto a server, and the worker back into needing one.
//
// The format is MOSIP's, so the constraint is interoperability rather than
// elegance: what CREST prints must be what Inji's wallet and any PixelPass
// reader expects. Implemented here rather than shelled out to the JavaScript
// library because a Go service cannot call npm, and pinned against that
// library's own output in pixelpass_test.go — the C19 lesson, where a codec
// agreeing with itself proved nothing.
//
// The layout is "NCF", then RFC 9285 base45 of a zlib stream WITH ITS TWO-BYTE
// HEADER REMOVED — the deflate data followed by its adler32, and nothing
// saying which compression settings produced it. That last part is not a
// guess: their library's output for "hello" is byte-identical to Go's zlib
// output minus the leading 78 9c. Decoding puts a header back, which is safe
// because a zlib header carries no information the deflate stream needs.
const pixelPassHeader = "NCF"

// zlibHeader is the standard "deflate, 32K window, default level" pair. Only
// its two constraints matter to a decoder — method 8, and (CMF<<8|FLG) % 31 ==
// 0 — so re-attaching this one to a stream compressed at any level is correct.
var zlibHeader = []byte{0x78, 0x9c}

// base45Alphabet is RFC 9285's, and the order is load-bearing.
const base45Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// ErrNotPixelPass is returned for a payload that is not a PixelPass document.
var ErrNotPixelPass = errors.New("not a PixelPass payload")

// EncodePixelPass turns a credential's JSON into the string a QR carries.
func EncodePixelPass(doc []byte) (string, error) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(doc); err != nil {
		return "", fmt.Errorf("compress credential: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress credential: %w", err)
	}
	body := compressed.Bytes()
	if len(body) < len(zlibHeader) {
		return "", errors.New("compressed credential is impossibly short")
	}
	return pixelPassHeader + base45Encode(body[len(zlibHeader):]), nil
}

// DecodePixelPass reverses it.
//
// Present so that what we print can be read back by this code as well as by
// theirs. A card that renders and decodes to something else fails in a worker's
// hands, somewhere nobody can debug it.
func DecodePixelPass(payload string) ([]byte, error) {
	if !strings.HasPrefix(payload, pixelPassHeader) {
		return nil, fmt.Errorf("%w: no %q header", ErrNotPixelPass, pixelPassHeader)
	}
	raw, err := base45Decode(payload[len(pixelPassHeader):])
	if err != nil {
		return nil, err
	}
	zr, err := zlib.NewReader(bytes.NewReader(append(append([]byte{}, zlibHeader...), raw...)))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPixelPass, err)
	}
	defer func() { _ = zr.Close() }()

	// Bounded: a printed card is a few kilobytes, and a decompression bomb
	// arriving as a QR code is still a decompression bomb.
	out, err := io.ReadAll(io.LimitReader(zr, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return out, nil
}

// base45Encode is RFC 9285. Two bytes become three characters; a trailing odd
// byte becomes two.
func base45Encode(in []byte) string {
	var b strings.Builder
	b.Grow(len(in)/2*3 + 2)
	for i := 0; i+1 < len(in); i += 2 {
		v := int(in[i])<<8 | int(in[i+1])
		b.WriteByte(base45Alphabet[v%45])
		b.WriteByte(base45Alphabet[(v/45)%45])
		b.WriteByte(base45Alphabet[v/45/45])
	}
	if len(in)%2 == 1 {
		v := int(in[len(in)-1])
		b.WriteByte(base45Alphabet[v%45])
		b.WriteByte(base45Alphabet[v/45])
	}
	return b.String()
}

func base45Decode(s string) ([]byte, error) {
	values := make([]int, 0, len(s))
	for _, c := range s {
		i := strings.IndexRune(base45Alphabet, c)
		if i < 0 {
			return nil, fmt.Errorf("%w: %q is not a base45 character", ErrNotPixelPass, c)
		}
		values = append(values, i)
	}

	var out []byte
	for len(values) >= 3 {
		v := values[0] + values[1]*45 + values[2]*45*45
		if v > 0xFFFF {
			return nil, fmt.Errorf("%w: a triple overflows two bytes", ErrNotPixelPass)
		}
		out = append(out, byte(v>>8), byte(v&0xFF))
		values = values[3:]
	}
	switch len(values) {
	case 0:
	case 2:
		v := values[0] + values[1]*45
		if v > 0xFF {
			return nil, fmt.Errorf("%w: a pair overflows one byte", ErrNotPixelPass)
		}
		out = append(out, byte(v))
	default:
		return nil, fmt.Errorf("%w: %d trailing characters", ErrNotPixelPass, len(values))
	}
	return out, nil
}
