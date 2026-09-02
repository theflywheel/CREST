package parties

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// The invitation state machine, tested where its decisions are made: pure
// functions over the offer document. Every table carries its refusals, because
// a state machine tested only on its happy path is one whose refusals are
// comments.
//
// Identifiers are synthetic DIDs in the ULID alphabet, as everywhere in these
// fixtures: nothing here may resemble a national identifier.
var (
	orgAmref     = "did:crest:party:01JCRESTAMREFKENYA00000000"
	orgProgramme = "did:crest:party:01JCRESTPRGRAMME0000000000"
	invProjectID = "crest:context:01JCRESTPRJ118000000000000"
	invitationID = "crest:invitation:01JCRESTNVTE00000000000000"
	sentAt       = time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	answeredAt   = time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
)

func endOfMarch() *time.Time {
	t := time.Date(2027, 3, 31, 23, 59, 59, 0, time.UTC)
	return &t
}

// prj118Invitation is g2_9's fixture: the offer PRJ-118 sends Amref, narrower
// than terms in period and functions, before anybody has answered it.
func prj118Invitation(t *testing.T) invitation {
	t.Helper()
	inv, ev, err := newInvitation(invitationID, invProjectID, orgAmref,
		[]string{"register-workers", "submit-evidence"},
		&schema.Period{End: endOfMarch()},
		"Bednet distribution, Kisumu wards 4 and 7", orgProgramme, sentAt)
	if err != nil {
		t.Fatalf("the reference offer must be sendable: %v", err)
	}
	if ev.Event != invEventSent || ev.ActorPartyID != orgProgramme {
		t.Fatalf("sending records a SENT event with the sender on it, got %+v", ev)
	}
	return inv
}

func TestAnOfferMustBeAnOfferOfSomethingThatEnds(t *testing.T) {
	period := &schema.Period{End: endOfMarch()}
	cases := []struct {
		name      string
		partyID   string
		functions []string
		period    *schema.Period
		note      string
		wantErr   bool
	}{
		{"the reference offer sends", orgAmref, []string{"register-workers"}, period, "", false},
		{"no invitee is no invitation", "", []string{"register-workers"}, period, "", true},
		{"an offer of nothing is refused", orgAmref, nil, period, "", true},
		{"a blank function is refused", orgAmref, []string{"register-workers", " "}, period, "", true},
		{"no period: the grant it would create must end", orgAmref, []string{"register-workers"}, nil, "", true},
		{"a period with no end is the same refusal", orgAmref, []string{"register-workers"}, &schema.Period{Start: sentAt}, "", true},
		{"a note the size of a document is refused", orgAmref, []string{"register-workers"}, period, strings.Repeat("x", maxInvitationNoteBytes+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv, _, err := newInvitation(invitationID, invProjectID, tc.partyID,
				tc.functions, tc.period, tc.note, orgProgramme, sentAt)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v, got %v", tc.wantErr, err)
			}
			if err == nil {
				if inv.State != invitationSent {
					t.Fatalf("a new offer is SENT, got %s", inv.State)
				}
				if inv.Period.Start.IsZero() {
					t.Fatal("an offer with no start begins when it is sent, not never")
				}
			}
		})
	}
}

func TestDecliningRecordsWhoAndWhyAndDestroysNothing(t *testing.T) {
	inv := prj118Invitation(t)
	declined, ev, err := decideInvitation(inv, false, "We cannot staff Kisumu this cycle", orgAmref, answeredAt)
	if err != nil {
		t.Fatalf("a decline with a reason is a first-class outcome: %v", err)
	}
	if declined.State != invitationDeclined || declined.Reason == nil || *declined.DecidedBy != orgAmref {
		t.Fatalf("the decline must carry its actor and reason: %+v", declined)
	}
	if ev.Event != invEventDeclined || ev.Note == nil {
		t.Fatalf("the trail row must carry the reason too: %+v", ev)
	}
	// The offer's substance survives the refusal.
	if declined.ContextID != invProjectID || len(declined.Functions) != 2 {
		t.Fatal("declining must not erase what was offered")
	}
}

func TestARefusalWithoutAReasonIsRefused(t *testing.T) {
	inv := prj118Invitation(t)
	if _, _, err := decideInvitation(inv, false, "  ", orgAmref, answeredAt); !errors.Is(err, errInvitationDeclineNeedsReason) {
		t.Fatalf("want errInvitationDeclineNeedsReason, got %v", err)
	}
	if _, _, err := decideInvitation(inv, false, strings.Repeat("r", maxDeclineReasonBytes+1), orgAmref, answeredAt); err == nil {
		t.Fatal("a reason the size of a document is not a reason")
	}
}

func TestASettledAnswerStaysSettled(t *testing.T) {
	inv := prj118Invitation(t)
	accepted, ev, err := decideInvitation(inv, true, "", orgAmref, answeredAt)
	if err != nil || accepted.State != invitationAccepted || ev.Event != invEventAccepted {
		t.Fatalf("acceptance: state=%s ev=%s err=%v", accepted.State, ev.Event, err)
	}
	// Every second answer, in every order, is the same refusal.
	for _, second := range []bool{true, false} {
		if _, _, err := decideInvitation(accepted, second, "changed our minds", orgAmref, answeredAt.Add(time.Hour)); !errors.Is(err, errInvitationDecided) {
			t.Fatalf("second answer (accept=%v) must be refused, got %v", second, err)
		}
	}
	declined, _, _ := decideInvitation(inv, false, "not this cycle", orgAmref, answeredAt)
	if _, _, err := decideInvitation(declined, true, "", orgAmref, answeredAt.Add(time.Hour)); !errors.Is(err, errInvitationDecided) {
		t.Fatalf("accept-after-decline must be refused, got %v", err)
	}
}

func TestQuestionsBelongToTheOpenOffer(t *testing.T) {
	inv := prj118Invitation(t)
	ev, err := askQuestion(inv, orgAmref, "Does the period extend if the campaign slips?", answeredAt)
	if err != nil || ev.Event != invEventQuestion || ev.Note == nil {
		t.Fatalf("a question on an open offer goes on the record: ev=%+v err=%v", ev, err)
	}
	if _, err := askQuestion(inv, orgAmref, "   ", answeredAt); err == nil {
		t.Fatal("a question with no words is refused")
	}
	accepted, _, _ := decideInvitation(inv, true, "", orgAmref, answeredAt)
	if _, err := askQuestion(accepted, orgAmref, "one more thing", answeredAt.Add(time.Hour)); !errors.Is(err, errQuestionAfterAnswer) {
		t.Fatalf("after the answer there is nobody holding the question open, got %v", err)
	}
}

// g2_5's whole point, as one table: registration stands alone, an offer may
// arrive before or after it, and only ACCEPTANCE waits for the registry's
// approval. The pure ordering rule is the same function the handler calls.
func TestRegistrationWorksBeforeOrAfterAnInvitation(t *testing.T) {
	cases := []struct {
		name     string
		regState string
		accepts  bool
	}{
		{"invited before registering: no registration yet, acceptance waits", "", false},
		{"applied but undecided: acceptance still waits", stateApplied, false},
		{"terms accepted, registry undecided: acceptance still waits", stateTermsAccepted, false},
		{"rejected: acceptance waits (a new decision, not this endpoint, changes that)", stateRejected, false},
		{"registered first, invited second: accepts", stateApproved, true},
		{"invited first, approved second: accepts — the offer did not expire with the wait", stateApproved, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mayAcceptInvitation(tc.regState)
			if tc.accepts && err != nil {
				t.Fatalf("an approved organisation accepts: %v", err)
			}
			if !tc.accepts && !errors.Is(err, errAcceptanceNeedsApproval) {
				t.Fatalf("want errAcceptanceNeedsApproval, got %v", err)
			}
			// Whatever the ordering, the offer itself was always sendable —
			// newInvitation takes no registration argument, by design.
			if _, _, err := newInvitation(invitationID, invProjectID, orgAmref,
				[]string{"register-workers"}, &schema.Period{End: endOfMarch()},
				"", orgProgramme, sentAt); err != nil {
				t.Fatalf("sending never waits on registration: %v", err)
			}
		})
	}
}
