package credential

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"time"
)

// The Bitstring Status List (§9).
//
// One bit per credential, meaning "still stands", and carrying nothing else. It
// is the single central fact about credentials, and it is deliberately the only
// one: there is no credential register (§3), because a register is a list of
// who did what that someone will eventually be asked to hand over.
//
// The list is bulk data and unlinkable by design. A verifier fetches the whole
// thing, so checking one credential's status reveals nothing about which
// credential was checked — which is why it is a bitstring rather than an
// endpoint you query by id.

// MinimumEntries is the floor on the list's size. A list barely larger than the
// number of credentials issued is a list whose size leaks how many there are,
// and whose set bits leak the revocation rate. 131,072 is the size the
// specification recommends as a minimum for exactly that reason.
const MinimumEntries = 131072

// StatusList is a revocation bitstring.
type StatusList struct {
	bits []byte
}

// NewStatusList builds a list with at least MinimumEntries slots.
func NewStatusList(entries int) *StatusList {
	if entries < MinimumEntries {
		entries = MinimumEntries
	}
	return &StatusList{bits: make([]byte, (entries+7)/8)}
}

// FromBytes rebuilds a list from stored bits.
func FromBytes(b []byte) *StatusList {
	if len(b) == 0 {
		return NewStatusList(MinimumEntries)
	}
	return &StatusList{bits: append([]byte(nil), b...)}
}

// Bytes returns the raw bitstring, for storage.
func (s *StatusList) Bytes() []byte { return append([]byte(nil), s.bits...) }

// Entries is how many credentials the list can describe.
func (s *StatusList) Entries() int { return len(s.bits) * 8 }

// Revoke sets the bit at index. Setting an already-set bit is not an error:
// revoking twice is a thing that happens, and it means the same thing.
func (s *StatusList) Revoke(index int) error {
	if index < 0 || index >= s.Entries() {
		return fmt.Errorf("status index %d is outside a list of %d", index, s.Entries())
	}
	s.bits[index/8] |= 1 << (7 - uint(index%8))
	return nil
}

// Revoked reports whether the bit at index is set.
func (s *StatusList) Revoked(index int) bool {
	if index < 0 || index >= s.Entries() {
		return false
	}
	return s.bits[index/8]&(1<<(7-uint(index%8))) != 0
}

// Encode returns the multibase-base64url gzip form the specification puts in
// the credential.
func (s *StatusList) Encode() (string, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(s.bits); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return "u" + base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// DecodeStatusList reverses Encode.
func DecodeStatusList(encoded string) (*StatusList, error) {
	if len(encoded) < 2 || encoded[0] != 'u' {
		return nil, fmt.Errorf("a status list must be multibase base64url ('u')")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded[1:])
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	var out bytes.Buffer
	if _, err := out.ReadFrom(zr); err != nil {
		return nil, err
	}
	return FromBytes(out.Bytes()), nil
}

// StatusListCredential wraps the bitstring as a signed credential, so a
// verifier can check the list came from the deployment rather than from
// whoever answered the URL.
func (i *Issuer) StatusListCredential(listURL string, list *StatusList, at time.Time) (map[string]any, error) {
	encoded, err := list.Encode()
	if err != nil {
		return nil, err
	}
	doc := map[string]any{
		"@context":  []any{ContextVC},
		"id":        listURL,
		"type":      []any{"VerifiableCredential", "BitstringStatusListCredential"},
		"issuer":    i.id,
		"validFrom": at.UTC().Format(time.RFC3339),
		"credentialSubject": map[string]any{
			"id":            listURL + "#list",
			"type":          "BitstringStatusList",
			"statusPurpose": "revocation",
			"encodedList":   encoded,
		},
	}
	return i.Issue(doc, at)
}
