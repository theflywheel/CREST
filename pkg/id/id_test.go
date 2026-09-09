package id_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/id"
)

// The pattern the schemas enforce. If these two ever disagree, every identifier
// this package mints fails validation at a service boundary — so they are
// checked against each other here rather than discovered in an integration test.
var ulid = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestULIDMatchesTheSchemaPattern(t *testing.T) {
	for i := 0; i < 200; i++ {
		got := id.ULID()
		if !ulid.MatchString(got) {
			t.Fatalf("%q does not match the schema's id pattern", got)
		}
	}
}

func TestIdentifiersAreDistinctAtTheSameInstant(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10_000; i++ {
		got := id.ULID()
		if seen[got] {
			t.Fatalf("collision at %q — ten thousand mints land inside the same "+
				"millisecond, so the randomness is the only thing keeping these apart", got)
		}
		seen[got] = true
	}
}

// Sorting by identifier must sort by mint time, because "the first claim of
// that batch" should be an ORDER BY rather than a join.
func TestIdentifiersSortByTime(t *testing.T) {
	earlier := id.ULID()
	// A real millisecond, because the timestamp half of a ULID has
	// millisecond resolution and there is no clock left to move.
	time.Sleep(2 * time.Millisecond)
	later := id.ULID()
	if earlier >= later {
		t.Errorf("%q should sort before %q", earlier, later)
	}
}

func TestPartyIsNeverAWorker(t *testing.T) {
	got := id.Party()
	if !regexp.MustCompile(`^did:crest:party:[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(got) {
		t.Fatalf("%q is not a Party DID", got)
	}
}
