package contract

import (
	"errors"
	"testing"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// The fixture world is the thing every other test builds on, so it is the
// first thing that has to be right. Load() validates every object against
// schemas/, which means this test fails the moment the world and a schema
// disagree — at load, with the field named.
func TestFixtureWorldLoadsAndValidates(t *testing.T) {
	w, err := fixtures.Load()
	if err != nil {
		t.Fatalf("load fixture world: %v", err)
	}

	if len(w.Parties) != 6 {
		t.Errorf("want 6 parties (org, specifier, supervisor, three workers), got %d", len(w.Parties))
	}

	// Three workers at three assurance levels is not decoration: the point of
	// the world is that the weakest worker is exercised alongside the strongest.
	for _, id := range []string{fixtures.WorkerAID, fixtures.WorkerBID, fixtures.WorkerCID} {
		if _, ok := w.Party(id); !ok {
			t.Errorf("worker %s missing from the world", id)
		}
	}
}

// Separation of duties is an L1 rule (§7). The fixture world would be a strange
// place to break the rule the services enforce, and a world that broke it would
// make every downstream test agree with a bug.
func TestFixtureDefinitionAuthorIsNotItsRatifier(t *testing.T) {
	d := fixtures.MustLoad().Definition()
	if d.RatifiedByPartyID == nil {
		t.Fatal("the ACTIVE fixture definition has no ratifier")
	}
	if d.AuthoredByPartyID == *d.RatifiedByPartyID {
		t.Errorf("author and ratifier are both %s — separation of duties (§7)", d.AuthoredByPartyID)
	}
}

// The tier map is read top to bottom and the first match wins, so the order is
// the semantics. A map whose floor is not last awards the floor to everything.
func TestFixtureTierMapDescendsToAFloor(t *testing.T) {
	rules := fixtures.MustLoad().Definition().TierMap
	for i := 1; i < len(rules); i++ {
		if rules[i].Tier >= rules[i-1].Tier {
			t.Fatalf("rule %d (tier %d) does not rank below rule %d (tier %d): "+
				"first-match-wins makes order the semantics", i, rules[i].Tier, i-1, rules[i-1].Tier)
		}
	}
	last := rules[len(rules)-1]
	if last.Tier != 1 {
		t.Errorf("the last rule is tier %d; a definition with no tier-1 floor rejects "+
			"evidence that §8 says is valid", last.Tier)
	}
	if len(last.RequiresFields) != 0 || last.MinIdentityAssurance != nil {
		t.Error("the tier-1 floor has conditions: a record providing only the mandatory " +
			"core must reach it (§8)")
	}
}

// Every schema in schemas/ must compile and be reachable by id. Adding a file
// is then enough; nobody has to remember to register it.
func TestEverySchemaCompiles(t *testing.T) {
	ids := schema.IDs()
	if len(ids) < 16 {
		t.Fatalf("expected at least the 11 primitives plus common, evidence, credential and "+
			"three profile payloads, got %d", len(ids))
	}
	// An empty object is invalid against every one of these, which is the
	// point: a validation failure proves the schema compiled and ran. Anything
	// else means the schema itself is broken.
	for _, id := range ids {
		err := schema.Validate(id, map[string]any{})
		if err == nil {
			continue
		}
		var ve *schema.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: did not compile: %v", id, err)
		}
	}
}
