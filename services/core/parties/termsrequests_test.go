package parties

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The terms-upgrade request machine (g2_6–g2_8, g2_11, g2_12), pure functions
// only. Refusals are the point: withdraw-after-decide, decide-a-draft,
// self-approval and a document smuggled in as a "ref" are each somebody's real
// mistake waiting to happen.
var (
	orgUpgrading  = "did:crest:party:01JCRESTAMREFKENYA00000000"
	regDecider    = "did:crest:party:01JCRESTREGSTRAR0000000000"
	signatoryPete = "did:crest:party:01JCRESTPETER0000000000000"
	requestID     = "crest:terms-request:01JCRESTWDRTRMS00000000000"
	widerTermsID  = "crest:terms:01JCRESTFLLDELVRY000000000"
	draftedAt     = time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	sentForReview = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	reviewedAt    = time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
)

// declaredCertificates is g2_7's list: kinds from the deployment's own
// taxonomy, refs into its own document custody, never content and never an
// identity document.
func declaredCertificates() []declaredDocument {
	return []declaredDocument{
		{Kind: "registration-certificate", Ref: "docstore://amref/reg-cert-2019", Hash: "b1946ac92492d2347c6235b4d2611184"},
		{Kind: "data-protection-officer", Ref: "docstore://amref/dpo-letter", Note: "named who answers for worker data"},
	}
}

func widerTermsRequest(t *testing.T) termsRequest {
	t.Helper()
	req, ev, err := newTermsRequest(requestID, orgUpgrading, widerTermsID, 3,
		declaredCertificates(), signatoryPete, draftedAt)
	if err != nil {
		t.Fatalf("the reference request must open: %v", err)
	}
	if req.State != requestDraft || ev.Event != reqEventDrafted || ev.ActorPartyID != signatoryPete {
		t.Fatalf("a new request is a DRAFT with its author on the trail: %+v / %+v", req, ev)
	}
	return req
}

func submitted(t *testing.T) termsRequest {
	t.Helper()
	req, ev, err := submitTermsRequest(widerTermsRequest(t), signatoryPete, sentForReview)
	if err != nil || req.State != requestSubmitted || ev.Event != reqEventSubmitted {
		t.Fatalf("submit: %+v ev=%+v err=%v", req, ev, err)
	}
	return req
}

func TestARequestNamesAPublishedVersionExactly(t *testing.T) {
	if _, _, err := newTermsRequest(requestID, orgUpgrading, "", 3, nil, signatoryPete, draftedAt); err == nil {
		t.Fatal("no terms id: a request for nothing must be refused")
	}
	if _, _, err := newTermsRequest(requestID, orgUpgrading, widerTermsID, 0, nil, signatoryPete, draftedAt); err == nil {
		t.Fatal("version 0 means the caller made a mistake, not version 1")
	}
}

func TestADeclaredDocumentIsAReferenceNeverTheDocument(t *testing.T) {
	cases := []struct {
		name string
		doc  declaredDocument
	}{
		{"no kind declares under no name", declaredDocument{Ref: "docstore://x"}},
		{"no ref declares nothing", declaredDocument{Kind: "registration-certificate"}},
		{"a multi-line ref is content wearing a reference's name",
			declaredDocument{Kind: "registration-certificate", Ref: "-----BEGIN CERT-----\nMIIB..."}},
		{"a ref beyond the bound is a document, not a pointer",
			declaredDocument{Kind: "registration-certificate", Ref: strings.Repeat("a", 501)}},
		{"a kind with spaces is not a slug", declaredDocument{Kind: "my certificate", Ref: "docstore://x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := newTermsRequest(requestID, orgUpgrading, widerTermsID, 3,
				[]declaredDocument{tc.doc}, signatoryPete, draftedAt); err == nil {
				t.Fatal("want a refusal")
			}
		})
	}
}

func TestOnlyADraftIsEditable(t *testing.T) {
	req := widerTermsRequest(t)
	edited, err := replaceDocuments(req, declaredCertificates()[:1])
	if err != nil || len(edited.Documents) != 1 {
		t.Fatalf("a draft's documents are the draft's to edit: %v", err)
	}
	sub := submitted(t)
	if _, err := replaceDocuments(sub, nil); !errors.Is(err, errRequestNotDraft) {
		t.Fatalf("after submission the reviewer must see what was submitted, got %v", err)
	}
	if _, _, err := submitTermsRequest(sub, signatoryPete, sentForReview.Add(time.Hour)); err == nil {
		t.Fatal("a second submit of the same request is a mistake, not an update")
	}
}

func TestWithdrawIsTheOrganisationsOwnActWhileItWaits(t *testing.T) {
	req, ev, err := withdrawTermsRequest(submitted(t), signatoryPete, "board asked us to wait a quarter", reviewedAt)
	if err != nil || req.State != requestWithdrawn || ev.Event != reqEventWithdrawn {
		t.Fatalf("withdraw while SUBMITTED: %+v err=%v", req, err)
	}
	if *req.DecidedBy != signatoryPete || req.Reason == nil {
		t.Fatalf("the withdrawal carries its actor (and here, its reason): %+v", req)
	}
	// A draft is not withdrawn, it is simply never submitted.
	if _, _, err := withdrawTermsRequest(widerTermsRequest(t), signatoryPete, "", reviewedAt); !errors.Is(err, errRequestNotSubmitted) {
		t.Fatalf("withdrawing a draft, got %v", err)
	}
	// A decided request is somebody else's answer; withdrawal cannot erase it.
	approvedReq, _, _ := decideTermsRequest(submitted(t), true, regDecider, "", reviewedAt)
	if _, _, err := withdrawTermsRequest(approvedReq, signatoryPete, "", reviewedAt.Add(time.Hour)); !errors.Is(err, errRequestDecided) {
		t.Fatalf("withdraw-after-decide, got %v", err)
	}
}

func TestTheDecisionHasANameOnItAndItIsNeverTheApplicants(t *testing.T) {
	sub := submitted(t)
	approvedReq, ev, err := decideTermsRequest(sub, true, regDecider, "", reviewedAt)
	if err != nil || approvedReq.State != requestApproved || ev.Event != reqEventApproved {
		t.Fatalf("approval: %+v err=%v", approvedReq, err)
	}
	if *approvedReq.DecidedBy != regDecider {
		t.Fatal("the approval carries the decider's name")
	}
	denied, ev, err := decideTermsRequest(sub, false, regDecider, "the DPO letter names nobody", reviewedAt)
	if err != nil || denied.State != requestDenied || ev.Reason == nil {
		t.Fatalf("denial with a reason: %+v err=%v", denied, err)
	}
	if _, _, err := decideTermsRequest(sub, false, regDecider, "", reviewedAt); !errors.Is(err, errDenialNeedsReason) {
		t.Fatalf("a denial without a reason is a closed door with no explanation, got %v", err)
	}
	if _, _, err := decideTermsRequest(sub, true, orgUpgrading, "", reviewedAt); !errors.Is(err, errRequestSelfDecided) {
		t.Fatalf("self-approval, got %v", err)
	}
	if _, _, err := decideTermsRequest(widerTermsRequest(t), true, regDecider, "", reviewedAt); !errors.Is(err, errRequestNotSubmitted) {
		t.Fatalf("deciding a draft decides something nobody submitted, got %v", err)
	}
	if _, _, err := decideTermsRequest(approvedReq, false, regDecider, "second thoughts", reviewedAt.Add(time.Hour)); !errors.Is(err, errRequestDecided) {
		t.Fatalf("a settled decision stays settled, got %v", err)
	}
}

func TestACheckVerdictHasABinaryOutcomeAndAnOwner(t *testing.T) {
	sub := submitted(t)
	v, err := newCheckVerdict(sub, "business-register", checkPass, checkOwnerPolicy,
		"crest:policy:business-register", "matched company no. verbatim", regDecider, reviewedAt)
	if err != nil || v.Outcome != checkPass || v.Owner != "crest:policy:business-register" {
		t.Fatalf("a policy-owned PASS records: %+v err=%v", v, err)
	}
	if _, err := newCheckVerdict(sub, "dpo-named", checkFail, checkOwnerParty, regDecider,
		"letter names an office, not a person", regDecider, reviewedAt); err != nil {
		t.Fatalf("a party-owned FAIL is just as recordable: %v", err)
	}
	cases := []struct {
		name                             string
		check, outcome, ownerKind, owner string
		recordedBy                       string
	}{
		{"an outcome that is neither passed nor failed did not run", "business-register", "MAYBE", checkOwnerPolicy, "crest:policy:x", regDecider},
		{"an owner kind that is neither party nor policy", "business-register", checkPass, "robot", "r2", regDecider},
		{"a verdict nobody owns", "business-register", checkPass, checkOwnerPolicy, " ", regDecider},
		{"the applicant does not own verdicts on its own request", "business-register", checkPass, checkOwnerParty, orgUpgrading, regDecider},
		{"nor record them", "business-register", checkPass, checkOwnerPolicy, "crest:policy:x", orgUpgrading},
		{"a check with no name checked nothing", "", checkPass, checkOwnerPolicy, "crest:policy:x", regDecider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newCheckVerdict(sub, tc.check, tc.outcome, tc.ownerKind, tc.owner,
				"", tc.recordedBy, reviewedAt); err == nil {
				t.Fatal("want a refusal")
			}
		})
	}
	// A check belongs to the review: not before submission, not after the answer.
	if _, err := newCheckVerdict(widerTermsRequest(t), "business-register", checkPass,
		checkOwnerPolicy, "crest:policy:x", "", regDecider, reviewedAt); !errors.Is(err, errRequestNotSubmitted) {
		t.Fatalf("a check on a draft examines a moving target, got %v", err)
	}
	decided, _, _ := decideTermsRequest(sub, true, regDecider, "", reviewedAt)
	if _, err := newCheckVerdict(decided, "business-register", checkPass,
		checkOwnerPolicy, "crest:policy:x", "", regDecider, reviewedAt.Add(time.Hour)); !errors.Is(err, errRequestNotSubmitted) {
		t.Fatalf("a check after the decision checked nothing the decision used, got %v", err)
	}
}
