// Package id mints CREST identifiers.
//
// One shape, everywhere: crest:<type>:<ULID>, and did:crest:party:<ULID> for a
// Party. ULIDs rather than UUIDs because they sort by creation time, which
// makes "the first claim of that batch" a query rather than a join, and because
// a human reading a log can tell two of them apart at a glance.
//
// The timestamp comes from the injected clock. That is not pedantry: the
// harness runs a seven-day window in milliseconds, and identifiers minted from
// wall-clock time inside it would sort in an order the test never asked for.
package id

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/theflywheel/crest/pkg/clock"
)

// Crockford base32: no I, L, O or U, so a ULID cannot be misread aloud or
// mistyped into a different valid one.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New mints an identifier of the given type: id.New(clk, "unit").
func New(clk clock.Clock, kind string) string {
	return "crest:" + kind + ":" + ULID(clk)
}

// Party mints a Party DID. Separate from New because a Party's identifier is a
// DID rather than a crest: URI, and because "worker" must never appear in it —
// worker is a role a Party holds (§2), and an identifier that names a role
// cannot be reused when the same person is a supervisor.
func Party(clk clock.Clock) string {
	return "did:crest:party:" + ULID(clk)
}

// ULID returns a 26-character Crockford base32 ULID: 48 bits of millisecond
// timestamp then 80 bits of randomness.
func ULID(clk clock.Clock) string {
	ms := clk.Now().UTC().UnixMilli()
	var raw [16]byte
	for i := 0; i < 6; i++ {
		raw[5-i] = byte(ms >> (8 * i))
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		// crypto/rand failing is not a condition a caller can do anything
		// useful with, and continuing would mint colliding identifiers for
		// records that decide whether someone gets paid.
		panic(fmt.Sprintf("id: no randomness available: %v", err))
	}

	// 128 bits into 26 base32 characters. 26*5 is 130, so the sequence is read
	// as if it were prefixed with two zero bits — which is the standard ULID
	// encoding, and the reason the first character is never above 7.
	bit := func(n int) uint8 {
		if n < 2 {
			return 0
		}
		n -= 2
		return (raw[n/8] >> (7 - n%8)) & 1
	}
	var b strings.Builder
	b.Grow(26)
	for i := 0; i < 26; i++ {
		var v uint8
		for j := 0; j < 5; j++ {
			v = v<<1 | bit(i*5+j)
		}
		b.WriteByte(alphabet[v])
	}
	return b.String()
}
