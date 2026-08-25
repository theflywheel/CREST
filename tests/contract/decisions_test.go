package contract

import (
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
