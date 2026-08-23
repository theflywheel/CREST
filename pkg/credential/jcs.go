package credential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// JCS canonicalisation (RFC 8785), which is what the eddsa-jcs-2022 cryptosuite
// signs over.
//
// Why not JSON-LD's RDF canonicalisation, which §5's "JSON-LD/Ed25519" implies?
// URDNA2015 needs a full JSON-LD processor and a resolvable context, and an
// offline verifier that must fetch a context document to check a signature is
// not offline (W6). JCS signs the bytes as written, which a verifier with no
// network can reproduce. This is a real divergence from the blueprint's default
// and is recorded as such rather than glossed.
//
// The limit worth knowing: RFC 8785 sorts keys by UTF-16 code unit and this
// sorts by byte. They agree for every key that is ASCII, which every key in
// schemas/ is — a contract test asserts it, so a non-ASCII key becomes a build
// failure rather than a signature that verifies on one implementation and not
// another.

// Canonicalise returns the JCS form of a JSON document.
func Canonicalise(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep 12 as 12, not 1.2e+01
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("canonicalise: %w", err)
	}
	var out bytes.Buffer
	if err := write(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// CanonicaliseValue marshals v and canonicalises the result.
func CanonicaliseValue(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Canonicalise(raw)
}

func write(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if !isASCII(k) {
				return fmt.Errorf("canonicalise: key %q is not ASCII, and this implementation "+
					"sorts by byte rather than by UTF-16 code unit", k)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := write(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := write(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case string:
		return writeString(b, t)
	case json.Number:
		b.WriteString(t.String())
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	default:
		return fmt.Errorf("canonicalise: unexpected %T", v)
	}
	return nil
}

// writeString escapes a string the way JCS does — which is the way
// JSON.stringify does, and which is *not* the way Go's json.Marshal does.
//
// json.Marshal HTML-escapes <, > and & into \u003c, \u003e and \u0026, and
// escapes U+2028/U+2029. RFC 8785 does none of that. Both sides of CREST used
// the same canonicaliser, so the signature round-tripped internally and nothing
// failed — but a standards-conformant JCS implementation, which is exactly what
// an independent offline verifier would use, produces different bytes and
// therefore a different hash. One "&" in an activity name or a geography field
// and the credential becomes unverifiable by anyone outside this codebase,
// which is the whole property the package exists to provide.
//
// Latent until a fixture contained one of those characters. Found by review.
func writeString(b *bytes.Buffer, s string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	// Encode appends a newline; the canonical form has no trailing whitespace.
	b.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	return nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return !strings.ContainsAny(s, "\x00")
}
