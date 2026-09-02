package parties

// The recovery nomination and refusal rules, tested as the pure functions
// they are (w1_7, w4_3). The refusal table is the point: w4_3's reference
// screen draws "a refusal, and no defined path after it", and the defined
// path starts with a refusal that cannot be recorded without an owner and a
// reason.

import (
	"errors"
	"testing"
)

var (
	rcWorker  = "did:crest:party:01JCRESTWORKERAMINA0000000"
	rcContact = "did:crest:party:01JCRESTCONTACTFATUMA00000"
)

func TestANominationNamesSomebodyElse(t *testing.T) {
	cases := []struct {
		name    string
		contact string
		wantErr error
	}{
		{"the reference nomination stands", rcContact, nil},
		{"nobody named is nobody to route to", "", errNoContactNamed},
		{"the worker cannot be their own recovery contact", rcWorker, errSelfNomination},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := nominationAdmissible(rcWorker, tc.contact); !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestARefusalIsARecordedAnswerNotADeadEnd(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		refuser string
		reason  string
		wantErr error
	}{
		{"a contacted person's no, with why, stands", "OPEN", rcContact,
			"the person who called me is not who they claimed", nil},
		{"a refusal without a reason is refused", "OPEN", rcContact, "", errRefusalNeedsReason},
		{"the person being recovered does not answer their own recovery", "OPEN", rcWorker,
			"any reason", errSelfConfirmation},
		{"a decided recovery is not collecting answers", "CONFIRMED", rcContact,
			"too late", errRecoveryNotOpen},
		{"an overridden recovery is not collecting answers", "OVERRIDDEN", rcContact,
			"too late", errRecoveryNotOpen},
		{"a completed recovery is not collecting answers", "COMPLETED", rcContact,
			"too late", errRecoveryNotOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := refusalAdmissible(tc.state, tc.refuser, rcWorker, tc.reason)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestConfidenceCheckIsANamedEnrolmentMethod(t *testing.T) {
	// w1_4: the no-document route is a provenance fact on the assisted
	// enrolment, not a new binding class — a confidence-checked worker's
	// assurance stays derived from their own identityBindings (IA-0 until a
	// route is verified or an anchor binds), and upgrades later with nothing
	// rewritten.
	for _, m := range []string{"supervisor-attested", "roster-import", "field-visit", "confidence-check"} {
		if !validEnrolmentMethod(m) {
			t.Fatalf("%q must be a valid enrolment method", m)
		}
	}
	for _, m := range []string{"", "self", "biometric", "document-seen"} {
		if validEnrolmentMethod(m) {
			t.Fatalf("%q must not be a valid enrolment method", m)
		}
	}
}
