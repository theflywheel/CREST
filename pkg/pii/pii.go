// Package pii holds the one operation CREST performs on a raw national
// identifier, and the reason it performs only that one.
//
// W9: never persist a raw national ID or biometric — a pairwise subject
// reference and a salted hash, nothing else, and that applies to fixtures too.
// A raw identifier exists in this system for exactly as long as it takes to
// hash it, and there is deliberately no function here that reverses, stores or
// logs one.
//
// The salt is per deployment and lives only in the deployment's secrets. Its
// purpose is not secrecy of the hash — it is that a national identifier space
// is small and enumerable, so an unsalted hash of one is the identifier with
// extra steps. A salt that leaks with the database is the failure this is
// guarding against, which is why it is configuration rather than a column.
package pii

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Hasher turns a raw identifier into the salted hash CREST stores.
type Hasher struct {
	salt    []byte
	saltRef string
}

// NewHasher builds a hasher. saltRef names which salt was used, so that a salt
// rotation is expressible: rows carry the reference, and a rotation adds a new
// one rather than invalidating every match at once.
func NewHasher(salt, saltRef string) (*Hasher, error) {
	if len(salt) < 16 {
		return nil, fmt.Errorf("pii: the salt is %d bytes; a short salt on an enumerable "+
			"identifier space is no salt at all", len(salt))
	}
	if saltRef == "" {
		return nil, fmt.Errorf("pii: a salt needs a reference, or a rotation cannot be expressed")
	}
	return &Hasher{salt: []byte(salt), saltRef: saltRef}, nil
}

// SaltRef is the identifier of the salt in use.
func (h *Hasher) SaltRef() string { return h.saltRef }

// Hash returns the hex-encoded HMAC-SHA256 of a normalised identifier.
//
// HMAC rather than a plain hash of salt+value: it is the construction designed
// for a secret key, and it removes any need to reason about length-extension on
// a value whose format varies by country.
func (h *Hasher) Hash(raw string) string {
	mac := hmac.New(sha256.New, h.salt)
	mac.Write([]byte(Normalise(raw)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Normalise removes the formatting that differs between systems presenting the
// same identifier — spaces, hyphens, case. Without it, "1234 5678" and
// "1234-5678" are two people, and the duplicate is invisible because both
// hashes are perfectly valid.
func Normalise(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch r {
		case ' ', '-', '/', '.', '\t':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
