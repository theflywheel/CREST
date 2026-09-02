package verification

// The share-request state machine, tested where its decisions are made: pure
// functions, every table carrying its refusals. These are w1_15 ("consent,
// per share, every time"), w1_19 ("who is asking, and why, before you
// decide") and w1_20 ("the worker sees the same list the verifier does") as
// executable statements.
//
// Identifiers are synthetic, in the ULID alphabet, resembling nothing
// national — the fixture rule everywhere in this repo.

import (
	"errors"
	"testing"
	"time"
)

var (
	shrWorker   = "did:crest:party:01JCRESTWORKERAMINA0000000"
	shrVerifier = "did:crest:party:01JCRESTVERIFIERLENDER0000"
	shrID       = "crest:share-request:01JCRESTSHRQ00000000000000"
	shrAskedAt  = time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	shrTTL      = 72 * time.Hour

	shrCredA = "crest:credential:01JCRESTCREDA0000000000000"
	shrCredB = "crest:credential:01JCRESTCREDB0000000000000"
	shrCredC = "crest:credential:01JCRESTCREDC0000000000000"
)

func openShare(t *testing.T) shareRequest {
	t.Helper()
	s, err := newShareRequest(shrID, shrWorker, shrVerifier,
		"Assessing a small loan application you made with us", nil, shrAskedAt, shrTTL)
	if err != nil {
		t.Fatalf("the reference request must be creatable: %v", err)
	}
	return s
}

func TestARequestNamesWhoIsAskingAndWhy(t *testing.T) {
	// w1_19: no purpose, a codebook purpose, an essay, and asking about
	// yourself are all refused at the machine, before any store is touched.
	cases := []struct {
		name        string
		subject     string
		requestedBy string
		purpose     string
		wantErr     error
	}{
		{"the reference request stands", shrWorker, shrVerifier,
			"Assessing a small loan application you made with us", nil},
		{"no purpose is no consent to give", shrWorker, shrVerifier, "", errShareNeedsPurpose},
		{"a code is not a reason a worker can read", shrWorker, shrVerifier, "KYC-7", errSharePurposeSize},
		{"an essay is not a reason either", shrWorker, shrVerifier,
			string(make([]rune, 201)), errSharePurposeSize},
		{"no subject is nobody to ask", "", shrVerifier, "Assessing a small loan application", errShareNeedsPurpose},
		{"no requester is nobody asking", shrWorker, "", "Assessing a small loan application", errShareNeedsPurpose},
		{"a worker does not petition themselves", shrWorker, shrWorker,
			"Assessing a small loan application", errShareSelfRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newShareRequest(shrID, tc.subject, tc.requestedBy, tc.purpose, nil, shrAskedAt, shrTTL)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestTheWorkerApprovesOnlyFromTheListTheyWereShown(t *testing.T) {
	// w1_20's outer bound: the disclosure list shown is the most any approval
	// can release. Approving a credential the list never carried is refused.
	shown := []string{shrCredA, shrCredB}
	decidedAt := shrAskedAt.Add(time.Hour)

	cases := []struct {
		name     string
		approve  bool
		approved []string
		reason   string
		wantErr  error
		wantSt   string
	}{
		{"a subset shares exactly that subset", true, []string{shrCredA}, "", nil, shareApproved},
		{"the whole list shares the whole list", true, shown, "", nil, shareApproved},
		{"a credential never shown cannot be approved", true, []string{shrCredC}, "", errShareNotRequested, ""},
		{"approving nothing is not an answer", true, nil, "", errShareNeedsList, ""},
		{"a refusal with a reason stands", false, nil, "I do not know this lender", nil, shareDeclined},
		{"a refusal without a reason is a dead end", false, nil, "", errShareRefuseReason, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decideShare(openShare(t), tc.approve, tc.approved, tc.reason, shown, decidedAt)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got.State != tc.wantSt {
				t.Fatalf("state %q, want %q", got.State, tc.wantSt)
			}
			if tc.wantErr == nil && tc.approve && len(got.ApprovedIDs) != len(tc.approved) {
				t.Fatalf("the approval must carry exactly the approved list, got %v", got.ApprovedIDs)
			}
		})
	}
}

func TestASettledAnswerStaysSettled(t *testing.T) {
	shown := []string{shrCredA}
	at := shrAskedAt.Add(time.Hour)
	approved, err := decideShare(openShare(t), true, shown, "", shown, at)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := decideShare(approved, false, nil, "changed my mind", shown, at.Add(time.Minute)); !errors.Is(err, errShareDecided) {
		t.Fatalf("re-deciding an approved share: got %v, want %v", err, errShareDecided)
	}
	declined, err := decideShare(openShare(t), false, nil, "I do not know this lender", shown, at)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	if _, err := decideShare(declined, true, shown, "", shown, at.Add(time.Minute)); !errors.Is(err, errShareDecided) {
		t.Fatalf("re-deciding a declined share: got %v, want %v", err, errShareDecided)
	}
}

func TestAnExpiredRequestIsNotAnswerableOrCollectable(t *testing.T) {
	// EXPIRED is derived, never stored: the same record reads REQUESTED
	// before the deadline and EXPIRED after it, and nothing wrote anything.
	s := openShare(t)
	late := shrAskedAt.Add(shrTTL + time.Minute)
	if got := s.effectiveState(shrAskedAt.Add(time.Hour)); got != shareRequested {
		t.Fatalf("before expiry the request reads %q, want %q", got, shareRequested)
	}
	if got := s.effectiveState(late); got != shareExpired {
		t.Fatalf("after expiry the request reads %q, want %q", got, shareExpired)
	}
	if _, err := decideShare(s, true, []string{shrCredA}, "", []string{shrCredA}, late); !errors.Is(err, errShareExpiredErr) {
		t.Fatalf("deciding late: got %v, want %v", err, errShareExpiredErr)
	}
	approved, err := decideShare(s, true, []string{shrCredA}, "", []string{shrCredA}, shrAskedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := collectShare(approved, late); !errors.Is(err, errShareExpiredErr) {
		t.Fatalf("collecting an expired approval: got %v, want %v", err, errShareExpiredErr)
	}
}

func TestConsentIsPerShareEveryTime(t *testing.T) {
	// w1_15: one approval, one collection. The second collect is refused —
	// there is no standing grant, and asking again is a new request.
	shown := []string{shrCredA}
	approved, err := decideShare(openShare(t), true, shown, "", shown, shrAskedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	collected, err := collectShare(approved, shrAskedAt.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if collected.State != shareFulfilled {
		t.Fatalf("state %q, want %q", collected.State, shareFulfilled)
	}
	if _, err := collectShare(collected, shrAskedAt.Add(3*time.Hour)); !errors.Is(err, errShareCollected) {
		t.Fatalf("second collect: got %v, want %v", err, errShareCollected)
	}
	// And nothing undecided or declined is collectable at all.
	if _, err := collectShare(openShare(t), shrAskedAt.Add(time.Hour)); !errors.Is(err, errShareNotApproved) {
		t.Fatalf("collecting an unanswered request: got %v, want %v", err, errShareNotApproved)
	}
	declined, err := decideShare(openShare(t), false, nil, "I do not know this lender", shown, shrAskedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("decline: %v", err)
	}
	if _, err := collectShare(declined, shrAskedAt.Add(2*time.Hour)); !errors.Is(err, errShareNotApproved) {
		t.Fatalf("collecting a declined request: got %v, want %v", err, errShareNotApproved)
	}
}
