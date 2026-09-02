package contract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/schema"
)

// Decisions that are negative — the things CREST records nowhere — are the
// ones prose cannot keep.
//
// An added field is visible in a diff and gets argued about. A decision that
// something must never exist is invisible until somebody adds it for a good
// local reason, and by then the argument is about removing a working feature
// rather than about the principle. So the ones that matter get a test that
// fails when they are violated, and the test carries the reasoning where
// whoever trips it will read it.

// declineWords are the ways a decline would most plausibly arrive in a schema.
//
// Word-boundary matched, because "declined" is also how a payment rail
// describes a rejected transfer and that is a different thing entirely — a
// fact about a bank, not about a worker's choice.
var declineWords = regexp.MustCompile(`(?i)\b(decline|declines|declined|declining|refusal_to_work|turned_down)\b`)

// exemptPaths are the places the word legitimately means something other than
// a worker's choice, by JSON pointer into the schema.
//
// One entry, and it earned it. Context.ownership is the handover
// acknowledgement (design finding F2): an Org Admin names a Project
// Configurator, and the named party accepts or declines. That decline is an
// organisation's authority act with a reason attached — the opposite of the
// conduct record §16 refuses, which is a worker's day-to-day answer to offered
// work following them between programmes. Nothing about a worker is recorded
// here and nothing in the payment path reads it.
//
// The exemption is a path rather than a whole schema on purpose. A future
// field on Context that DID record a worker declining work would still fail
// this test, which is what the guard is for.
var exemptPaths = map[string]string{
	"urn:crest:schema:primitives:context:1": "/properties/ownership",
}

// §16: a worker declining offered work is never recorded.
//
// CREST records work that happened. A decline history is a conduct record, and
// one that would follow a worker between programmes for reasons the record
// cannot hold and should not imply — illness, care, a funeral, a day somebody
// simply said no. Nothing in the payment path needs it.
//
// Recording it worker-visible-only was considered and rejected, because data
// that exists gets requested and a visibility rule is one export away from not
// holding. The only durable version of "not visible to a programme" is "not
// there".
//
// If this test fails, the question is not how to make the field pass it. It is
// whether §16's ruling has changed, which is a blueprint edit and a
// conversation, not a schema edit.
func TestDecliningWorkIsRecordedNowhere(t *testing.T) {
	for id, source := range schema.Sources {
		// Payment-rail vocabulary legitimately says "declined" about a
		// transfer. Only the primitives that describe a worker or their work
		// are in scope for this rule.
		if strings.Contains(id, "payment") || strings.Contains(id, "rail") {
			continue
		}
		if path, exempt := exemptPaths[id]; exempt {
			var err error
			if source, err = withoutSubtree(source, path); err != nil {
				t.Fatalf("%s: removing the exempt subtree %s: %v", id, path, err)
			}
		}
		if match := declineWords.FindString(source); match != "" {
			t.Errorf("%s mentions %q.\n\n"+
				"A worker declining offered work is recorded nowhere (Blueprint §16). "+
				"CREST records work that happened; a decline history is a conduct record "+
				"that follows a worker between programmes for reasons the record cannot "+
				"hold. If the ruling has changed, change the blueprint first — this test "+
				"is downstream of it, not a rule of its own.", id, match)
		}
	}
}

// withoutSubtree re-serialises a schema with one subtree removed, so an
// exemption is a named path rather than a whole file nobody checks any more.
//
// It deletes rather than blanks the subtree, and it fails loudly if the path
// is not there: an exemption pointing at a property that has been renamed
// would otherwise quietly stop exempting anything, or quietly stop checking
// everything, and it is not obvious from the outside which.
func withoutSubtree(source, pointer string) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(source), &doc); err != nil {
		return "", err
	}
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	cursor := doc
	for _, seg := range segments[:len(segments)-1] {
		next, ok := cursor[seg].(map[string]any)
		if !ok {
			return "", fmt.Errorf("no object at %q", seg)
		}
		cursor = next
	}
	last := segments[len(segments)-1]
	if _, there := cursor[last]; !there {
		return "", fmt.Errorf("nothing to exempt at %q; has it been renamed?", last)
	}
	delete(cursor, last)
	out, err := json.Marshal(doc)
	return string(out), err
}
