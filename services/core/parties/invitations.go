// Invitations: the G-2 "a project has invited you" surface (g2_5, g2_9, g2_10).
//
// An invitation is the OFFER whose acceptance creates a partner grant. It is
// deliberately not a new primitive: the thing that outlives the conversation is
// the Authorization the acceptance creates (the same context-scoped grant
// p2_18's endpoint writes), and the invitation is the recorded offer-and-answer
// that produced it. Blueprint §15 J1 names this object: "project→org invitation
// object carrying a scoped grant (functions × places × period) with
// accept/decline".
//
// The layering test, applied: the functions offered are opaque L2 strings; the
// places live inside Period/configuration the offer carries verbatim; nothing
// here knows a sector, a country rule or a role vocabulary. What is
// infrastructure — and therefore here — is that an offer has an answer, the
// answer has an actor and (for a refusal) a reason, a settled answer stays
// settled, and the offer cannot mint a grant wider than the terms the invited
// organisation accepted.
//
// Ordering (g2_5's whole point): registration stands alone. An invitation may
// be SENT to an organisation whose registration is not yet decided — the party
// exists, the offer is a fact — but ACCEPTANCE is gated on the registry's own
// approval and on accepted terms, because acceptance creates the grant and a
// grant rides the terms. Both orders therefore work: register-then-be-invited,
// and be-invited-then-register. The gate lives in the handler, where the
// registration can be read; the state machine below is pure.
package parties

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// Invitation states. SENT → ACCEPTED or DECLINED, nothing else and no way
// back. There is no WITHDRAWN: the reference screens give the project no
// un-send button, and adding one un-asked would be scope wearing a state name.
const (
	invitationSent     = "SENT"
	invitationAccepted = "ACCEPTED"
	invitationDeclined = "DECLINED"
)

// Event verbs of the invitation trail. QUESTION is g2_9's "Ask a question":
// conversation on the record, not a state change.
const (
	invEventSent     = "SENT"
	invEventQuestion = "QUESTION"
	invEventAccepted = "ACCEPTED"
	invEventDeclined = "DECLINED"
)

const maxInvitationNoteBytes = 2000

var (
	// errInvitationDecided keeps a settled answer settled — the same posture
	// as errOwnershipAlreadyDecided on the handover.
	errInvitationDecided = errors.New("this invitation has already been answered")
	// errInvitationDeclineNeedsReason: a refusal without a reason is a dead
	// end, and this system does not leave people at dead ends.
	errInvitationDeclineNeedsReason = errors.New("declining an invitation records a reason")
	// errInvitationMustEnd mirrors grantPartner's rule at the offer stage: the
	// grant an acceptance creates carries an end date, so the offer must too —
	// otherwise the org would be accepting something the system then refuses.
	errInvitationMustEnd = errors.New("an invitation offers a grant, and a partner grant carries an end date; offer one")
	// errQuestionAfterAnswer: the conversation belongs to the open offer.
	errQuestionAfterAnswer = errors.New("this invitation has been answered; there is nobody holding the question open")
)

// invitation is one recorded offer from a project to an organisation.
type invitation struct {
	ID        string        `json:"id"`
	ContextID string        `json:"contextId"`
	PartyID   string        `json:"partyId"`
	Functions []string      `json:"functions"`
	Period    schema.Period `json:"period"`
	// Note is the inviting side's covering words, opaque and bounded.
	Note      string    `json:"note,omitempty"`
	State     string    `json:"state"`
	InvitedBy string    `json:"invitedBy"`
	InvitedAt time.Time `json:"invitedAt"`
	// The answer, when there is one. DecidedBy is the actor who answered —
	// recorded, never derived from PartyID, because somebody proven to act for
	// the organisation may answer on its behalf.
	DecidedBy *string    `json:"decidedBy,omitempty"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
	Reason    *string    `json:"reason,omitempty"`
	// GrantID names the Authorization the acceptance created — the walkable
	// link from the offer to the standing fact it became (g2_10).
	GrantID *string `json:"grantId,omitempty"`
}

// invitationEvent is one row of the invitation's append-only trail.
type invitationEvent struct {
	Seq          int       `json:"seq"`
	Event        string    `json:"event"`
	ActorPartyID string    `json:"actorPartyId"`
	Note         *string   `json:"note,omitempty"`
	At           time.Time `json:"at"`
}

// newInvitation validates and shapes the offer. It does not check who the
// organisation is — approval is the handler's read — but it does check the
// offer's own shape, because an offer the acceptance path must refuse is a
// trap, not an invitation.
func newInvitation(id, contextID, partyID string, functions []string,
	period *schema.Period, note, invitedBy string, at time.Time) (invitation, invitationEvent, error) {

	if partyID == "" {
		return invitation{}, invitationEvent{}, errors.New("an invitation names the organisation being invited")
	}
	if len(functions) == 0 {
		return invitation{}, invitationEvent{}, errors.New("an invitation offers at least one function; an offer of nothing asks a question it does not contain")
	}
	for _, f := range functions {
		if strings.TrimSpace(f) == "" {
			return invitation{}, invitationEvent{}, errors.New("an offered function is a named permission, not a blank")
		}
	}
	if period == nil || period.End == nil {
		return invitation{}, invitationEvent{}, errInvitationMustEnd
	}
	if len(note) > maxInvitationNoteBytes {
		return invitation{}, invitationEvent{}, fmt.Errorf("a covering note of %d bytes is a document, not a note", len(note))
	}
	p := *period
	if p.Start.IsZero() {
		p.Start = at
	}
	inv := invitation{
		ID: id, ContextID: contextID, PartyID: partyID,
		Functions: functions, Period: p, Note: strings.TrimSpace(note),
		State: invitationSent, InvitedBy: invitedBy, InvitedAt: at,
	}
	return inv, invitationEvent{Event: invEventSent, ActorPartyID: invitedBy, At: at}, nil
}

// decideInvitation records the organisation's answer. Declining is a
// first-class outcome with a required reason — it contests the offer, not the
// organisation's standing, and it deletes nothing.
func decideInvitation(inv invitation, accept bool, reason, actorPartyID string,
	at time.Time) (invitation, invitationEvent, error) {

	if inv.State != invitationSent {
		return inv, invitationEvent{}, errInvitationDecided
	}
	reason = strings.TrimSpace(reason)
	if !accept && reason == "" {
		return inv, invitationEvent{}, errInvitationDeclineNeedsReason
	}
	if len(reason) > maxDeclineReasonBytes {
		return inv, invitationEvent{}, fmt.Errorf("a reason of %d bytes is a document, not a reason", len(reason))
	}
	decided := at
	inv.DecidedAt = &decided
	inv.DecidedBy = &actorPartyID
	ev := invitationEvent{ActorPartyID: actorPartyID, At: at}
	if accept {
		inv.State = invitationAccepted
		ev.Event = invEventAccepted
	} else {
		inv.State = invitationDeclined
		inv.Reason = &reason
		ev.Event = invEventDeclined
		ev.Note = &reason
	}
	return inv, ev, nil
}

// errAcceptanceNeedsApproval is the ordering rule's refusal, said as an
// ordering fact rather than a dead end: the invitation stands, the acceptance
// waits.
var errAcceptanceNeedsApproval = errors.New(
	"this invitation stands, but accepting it creates a standing grant, and that " +
		"waits for the registry's approval of the organisation; register (or wait for " +
		"the decision) and accept then — the offer does not expire with the wait")

// mayAcceptInvitation is g2_5's ordering rule as one pure function: an
// invitation may be SENT before or after the organisation's registration is
// decided, but ACCEPTED only once the registration is APPROVED — because
// acceptance writes the partner grant, and a grant rides terms an approved
// registration holds. Any earlier state, and the absence of a registration
// entirely, get the same answer: not yet, and nothing is lost by the wait.
func mayAcceptInvitation(registrationState string) error {
	if registrationState != stateApproved {
		return errAcceptanceNeedsApproval
	}
	return nil
}

// askQuestion appends g2_9's "Ask a question" to the trail. It changes no
// state: a question is conversation on the record, and either side may ask
// while the offer is open. After the answer there is nobody holding the offer,
// so the trail closes with it.
func askQuestion(inv invitation, actorPartyID, text string, at time.Time) (invitationEvent, error) {
	if inv.State != invitationSent {
		return invitationEvent{}, errQuestionAfterAnswer
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return invitationEvent{}, errors.New("a question has words in it")
	}
	if len(text) > maxInvitationNoteBytes {
		return invitationEvent{}, fmt.Errorf("a question of %d bytes is a document, not a question", len(text))
	}
	return invitationEvent{Event: invEventQuestion, ActorPartyID: actorPartyID, Note: &text, At: at}, nil
}
