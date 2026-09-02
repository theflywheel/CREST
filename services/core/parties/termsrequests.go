// Terms-upgrade requests: the G-2 "request wider terms" surface (g2_6–g2_8,
// g2_11, g2_12).
//
// An organisation holding narrow terms asks for a wider *published* terms
// version. The request carries declared qualification documents, goes for
// review, is visible while it waits, can be withdrawn, and is approved or
// denied by a named decider — the same posture as the registration decision
// in onboarding.go. On approval the organisation's registration is moved to
// the requested terms version; nothing else about the registration changes.
//
// The layering test, applied. Which terms sets exist, what each requires an
// organisation to show, and which checks run before approval are all L2 — two
// deployments will disagree about every one of them. So a document's `kind` is
// an opaque slug from the deployment's own taxonomy, a check's `name` likewise,
// and no enum below encodes either. What is infrastructure: a request is to a
// published terms version that exists; documents are references, never content;
// every transition is appended with an actor and, for refusals, a reason; a
// settled decision stays settled; and the decider is never the applicant.
//
// Never persist a raw national ID or biometric — and, here, never a raw
// document at all. A declared document is {kind, ref, hash}: the reference into
// wherever the deployment keeps the bytes, and optionally the hash that pins
// what was seen. This service has no blob store and grows none; if real upload
// is needed it is a separate capability with its own custody answer, and its
// absence is a stated gap, not an oversight.
package parties

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Request states. DRAFT → SUBMITTED → APPROVED | DENIED | WITHDRAWN. No path
// re-opens a settled request: asking again is a new request, and the old
// answer survives as its own record.
const (
	requestDraft     = "DRAFT"
	requestSubmitted = "SUBMITTED"
	requestWithdrawn = "WITHDRAWN"
	requestApproved  = "APPROVED"
	requestDenied    = "DENIED"
)

// Event verbs of the request trail.
const (
	reqEventDrafted   = "DRAFTED"
	reqEventSubmitted = "SUBMITTED"
	reqEventWithdrawn = "WITHDRAWN"
	reqEventApproved  = "APPROVED"
	reqEventDenied    = "DENIED"
)

// Check outcomes. Binary on purpose: whether a named check passed is the one
// thing about a check two deployments cannot reasonably disagree on. What the
// checks ARE is their catalogue's business, not this file's.
const (
	checkPass = "PASS"
	checkFail = "FAIL"
)

// Who answers for a check's verdict. A `party` owner is a person or
// organisation in the registry; a `policy` owner is a named piece of
// deployment configuration (the same shape as approvalByPolicy). g2_12 says
// this stage "mostly runs without a person" — today no automated checker
// exists in this codebase, so every verdict is recorded through this door by
// whoever (or whatever) ran the check, and the owner field is where the
// eventual business-register adapter will put its policy name. Modelled as
// recorded verdicts rather than as fake automation, deliberately.
const (
	checkOwnerParty  = "party"
	checkOwnerPolicy = "policy"
)

var (
	errRequestNotDraft     = errors.New("only a DRAFT request can be edited or submitted")
	errRequestNotSubmitted = errors.New("only a SUBMITTED request can be withdrawn, checked or decided")
	errRequestDecided      = errors.New("this request has already been decided")
	errRequestSelfDecided  = errors.New("an organisation may not decide its own terms request")
	errDenialNeedsReason   = errors.New("a denial needs a reason")
)

// declaredDocument is one qualification document, declared rather than
// uploaded: a reference and optionally a hash, never the bytes.
type declaredDocument struct {
	// Kind is the deployment's own document taxonomy — "registration-certificate",
	// "data-protection-officer-letter" — an opaque slug here.
	Kind string `json:"kind"`
	// Ref is where the deployment's document custody keeps it. A reference,
	// bounded and single-line, so nothing document-shaped can be smuggled in.
	Ref string `json:"ref"`
	// Hash, when given, pins what the reviewer saw to what was declared.
	Hash string `json:"hash,omitempty"`
	Note string `json:"note,omitempty"`
}

// termsRequest is one organisation's request to move to a wider terms version.
type termsRequest struct {
	ID      string `json:"id"`
	PartyID string `json:"partyId"`
	// The published terms version being requested — required and exact, for
	// the same reason acceptTerms requires one: "whatever was current" is not
	// a fact a verifier can walk back to.
	TermsID      string             `json:"termsId"`
	TermsVersion int                `json:"termsVersion"`
	Documents    []declaredDocument `json:"documents"`
	State        string             `json:"state"`
	CreatedBy    string             `json:"createdBy"`
	CreatedAt    time.Time          `json:"createdAt"`
	SubmittedBy  *string            `json:"submittedBy,omitempty"`
	SubmittedAt  *time.Time         `json:"submittedAt,omitempty"`
	DecidedBy    *string            `json:"decidedBy,omitempty"`
	DecidedAt    *time.Time         `json:"decidedAt,omitempty"`
	Reason       *string            `json:"reason,omitempty"`
	// The terms the organisation held when the request was approved, captured
	// at approval. org_registrations holds only the CURRENT terms, and "which
	// terms did this organisation hold before the upgrade" must survive the
	// overwrite — this is where it does.
	PreviousTermsID      *string `json:"previousTermsId,omitempty"`
	PreviousTermsVersion *int    `json:"previousTermsVersion,omitempty"`
}

// termsRequestEvent is one row of the request's append-only trail.
type termsRequestEvent struct {
	Seq          int       `json:"seq"`
	Event        string    `json:"event"`
	ActorPartyID string    `json:"actorPartyId"`
	Reason       *string   `json:"reason,omitempty"`
	At           time.Time `json:"at"`
}

// checkVerdict is one recorded check on a submitted request (g2_12), with the
// two facts that make it accountable: the outcome and who answers for it.
type checkVerdict struct {
	Seq  int    `json:"seq"`
	Name string `json:"name"`
	// PASS or FAIL. A check that cannot say which did not run.
	Outcome   string `json:"outcome"`
	OwnerKind string `json:"ownerKind"`
	// Owner: a party id, or a policy name like "crest:policy:business-register".
	Owner      string    `json:"owner"`
	Note       string    `json:"note,omitempty"`
	RecordedBy string    `json:"recordedBy"`
	At         time.Time `json:"at"`
}

func validDocuments(docs []declaredDocument) error {
	for i, d := range docs {
		if err := validRecordKind(d.Kind); err != nil {
			return fmt.Errorf("document %d: %v", i+1, err)
		}
		ref := strings.TrimSpace(d.Ref)
		switch {
		case ref == "":
			return fmt.Errorf("document %d (%s): a declared document is a reference; one with no ref declares nothing", i+1, d.Kind)
		case len(ref) > 500 || strings.ContainsAny(ref, "\n\r"):
			// The bound is the guard: a multi-kilobyte or multi-line "ref" is
			// content wearing a reference's name, and content — least of all an
			// identity document — must never land in this store.
			return fmt.Errorf("document %d (%s): ref is a single-line reference, not the document itself", i+1, d.Kind)
		}
		if len(d.Hash) > 200 {
			return fmt.Errorf("document %d (%s): that is not a hash", i+1, d.Kind)
		}
		if len(d.Note) > maxInvitationNoteBytes {
			return fmt.Errorf("document %d (%s): a note is a note, not a covering letter", i+1, d.Kind)
		}
	}
	return nil
}

// newTermsRequest opens a DRAFT. The terms version is required here and its
// existence as a published Terms is the handler's read — a request for terms
// nobody published is a request for nothing.
func newTermsRequest(id, partyID, termsID string, termsVersion int,
	docs []declaredDocument, createdBy string, at time.Time) (termsRequest, termsRequestEvent, error) {

	if termsID == "" || termsVersion < 1 {
		return termsRequest{}, termsRequestEvent{}, errors.New(
			"a terms request names a published terms id and a positive version; \"whatever is current\" is not a requestable thing")
	}
	if err := validDocuments(docs); err != nil {
		return termsRequest{}, termsRequestEvent{}, err
	}
	if docs == nil {
		docs = []declaredDocument{}
	}
	req := termsRequest{
		ID: id, PartyID: partyID, TermsID: termsID, TermsVersion: termsVersion,
		Documents: docs, State: requestDraft, CreatedBy: createdBy, CreatedAt: at,
	}
	return req, termsRequestEvent{Event: reqEventDrafted, ActorPartyID: createdBy, At: at}, nil
}

// replaceDocuments is g2_7's "Save draft": the declared list is the draft's to
// edit, and only the draft's. After submission the reviewer must be looking at
// what was submitted.
func replaceDocuments(req termsRequest, docs []declaredDocument) (termsRequest, error) {
	if req.State != requestDraft {
		return req, errRequestNotDraft
	}
	if err := validDocuments(docs); err != nil {
		return req, err
	}
	if docs == nil {
		docs = []declaredDocument{}
	}
	req.Documents = docs
	return req, nil
}

// submitTermsRequest is g2_7's "Submit". Whether the declared documents are
// ENOUGH is the review's question (an L2 one); whether the request is a
// submittable thing is this function's.
func submitTermsRequest(req termsRequest, actor string, at time.Time) (termsRequest, termsRequestEvent, error) {
	if req.State != requestDraft {
		if req.State == requestSubmitted {
			return req, termsRequestEvent{}, errors.New("this request is already submitted")
		}
		return req, termsRequestEvent{}, errRequestNotDraft
	}
	submitted := at
	req.State = requestSubmitted
	req.SubmittedBy = &actor
	req.SubmittedAt = &submitted
	return req, termsRequestEvent{Event: reqEventSubmitted, ActorPartyID: actor, At: at}, nil
}

// withdrawTermsRequest is g2_8's "Withdraw". The organisation's own act, only
// while the request is waiting; a decided request is somebody else's answer
// and withdrawal cannot erase it. The reason is optional — withdrawing your
// own ask is not a refusal — but the actor is not.
func withdrawTermsRequest(req termsRequest, actor, reason string, at time.Time) (termsRequest, termsRequestEvent, error) {
	if req.State == requestApproved || req.State == requestDenied {
		return req, termsRequestEvent{}, errRequestDecided
	}
	if req.State != requestSubmitted {
		return req, termsRequestEvent{}, errRequestNotSubmitted
	}
	decided := at
	req.State = requestWithdrawn
	req.DecidedBy = &actor
	req.DecidedAt = &decided
	ev := termsRequestEvent{Event: reqEventWithdrawn, ActorPartyID: actor, At: at}
	if reason = strings.TrimSpace(reason); reason != "" {
		if len(reason) > maxDeclineReasonBytes {
			return req, termsRequestEvent{}, fmt.Errorf("a reason of %d bytes is a document, not a reason", len(reason))
		}
		req.Reason = &reason
		ev.Reason = &reason
	}
	return req, ev, nil
}

// decideTermsRequest approves or denies, with the registration decision's own
// rules: a named decider who is not the applicant, a reason on every denial,
// and a settled decision that stays settled.
func decideTermsRequest(req termsRequest, approve bool, decidedBy, reason string,
	at time.Time) (termsRequest, termsRequestEvent, error) {

	if req.State == requestApproved || req.State == requestDenied {
		return req, termsRequestEvent{}, errRequestDecided
	}
	if req.State != requestSubmitted {
		return req, termsRequestEvent{}, errRequestNotSubmitted
	}
	if decidedBy == req.PartyID {
		return req, termsRequestEvent{}, errRequestSelfDecided
	}
	reason = strings.TrimSpace(reason)
	if !approve && reason == "" {
		return req, termsRequestEvent{}, errDenialNeedsReason
	}
	if len(reason) > maxDeclineReasonBytes {
		return req, termsRequestEvent{}, fmt.Errorf("a reason of %d bytes is a document, not a reason", len(reason))
	}
	decided := at
	req.DecidedBy = &decidedBy
	req.DecidedAt = &decided
	ev := termsRequestEvent{ActorPartyID: decidedBy, At: at}
	if approve {
		req.State = requestApproved
		ev.Event = reqEventApproved
		if reason != "" {
			req.Reason = &reason
			ev.Reason = &reason
		}
	} else {
		req.State = requestDenied
		req.Reason = &reason
		ev.Event = reqEventDenied
		ev.Reason = &reason
	}
	return req, ev, nil
}

// newCheckVerdict validates one recorded check. Recording is allowed only
// while the request is SUBMITTED — a check on a draft examines a moving
// target, and a check after the decision checked nothing the decision used.
func newCheckVerdict(req termsRequest, name, outcome, ownerKind, owner, note,
	recordedBy string, at time.Time) (checkVerdict, error) {

	if req.State != requestSubmitted {
		return checkVerdict{}, errRequestNotSubmitted
	}
	if err := validRecordKind(name); err != nil {
		return checkVerdict{}, fmt.Errorf("check name: %v", err)
	}
	switch outcome {
	case checkPass, checkFail:
	default:
		return checkVerdict{}, fmt.Errorf("a check passed or it failed; %q is neither", outcome)
	}
	switch ownerKind {
	case checkOwnerParty, checkOwnerPolicy:
	default:
		return checkVerdict{}, fmt.Errorf("a verdict's owner is a party or a policy; %q is neither", ownerKind)
	}
	if strings.TrimSpace(owner) == "" {
		return checkVerdict{}, errors.New("a verdict nobody owns is a verdict nobody can be asked about")
	}
	// The applicant does not check itself. The same rule as ErrSelfApproved,
	// one stage earlier.
	if (ownerKind == checkOwnerParty && owner == req.PartyID) || recordedBy == req.PartyID {
		return checkVerdict{}, errors.New("an organisation may not record check verdicts on its own request")
	}
	if len(note) > maxInvitationNoteBytes {
		return checkVerdict{}, errors.New("a check note is a note, not a report")
	}
	return checkVerdict{
		Name: strings.TrimSpace(name), Outcome: outcome,
		OwnerKind: ownerKind, Owner: strings.TrimSpace(owner),
		Note: strings.TrimSpace(note), RecordedBy: recordedBy, At: at,
	}, nil
}
