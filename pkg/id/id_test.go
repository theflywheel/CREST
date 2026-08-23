package id_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/id"
)

// The pattern the schemas enforce. If these two ever disagree, every identifier
// this package mints fails validation at a service boundary — so they are
// checked against each other here rather than discovered in an integration test.
var ulid = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestULIDMatchesTheSchemaPattern(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	for i := 0; i < 200; i++ {
		got := id.ULID(clk)
		if !ulid.MatchString(got) {
			t.Fatalf("%q does not match the schema's id pattern", got)
		}
	}
}

func TestIdentifiersAreDistinctAtTheSameInstant(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	seen := map[string]bool{}
	for i := 0; i < 10_000; i++ {
		got := id.ULID(clk)
		if seen[got] {
			t.Fatalf("collision at %q — the clock does not advance in the harness, so "+
				"the randomness is the only thing keeping these apart", got)
		}
		seen[got] = true
	}
}

// Sorting by identifier must sort by mint time, because "the first claim of
// that batch" should be an ORDER BY rather than a join.
func TestIdentifiersSortByTime(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	earlier := id.ULID(clk)
	clk.Advance(time.Second)
	later := id.ULID(clk)
	if earlier >= later {
		t.Errorf("%q should sort before %q", earlier, later)
	}
}

func TestPartyIsNeverAWorker(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	got := id.Party(clk)
	if !regexp.MustCompile(`^did:crest:party:[0-9A-HJKMNP-TV-Z]{26}$`).MatchString(got) {
		t.Fatalf("%q is not a Party DID", got)
	}
}
