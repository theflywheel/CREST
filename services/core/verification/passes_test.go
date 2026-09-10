package verification

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A pass is a name the worker reads and a contact somebody can reach. Neither
// is optional, and neither is checked beyond being present and plausible —
// "no account, no vetting" is the design (J9 L1).
func TestAPassNeedsANameAndAReachableContact(t *testing.T) {
	name, contact, err := validatePassRequest("  Joseph Mwangi ", "+254 700 000 412")
	if err != nil {
		t.Fatalf("a sound request was refused: %v", err)
	}
	if name != "Joseph Mwangi" || contact != "+254700000412" {
		t.Errorf("normalised to %q / %q", name, contact)
	}

	for _, tc := range []struct{ name, contact, says string }{
		{"J", "+254700000412", "name must be"},
		{strings.Repeat("x", 81), "+254700000412", "name must be"},
		{"Joseph Mwangi", "", "contact must be"},
		{"Joseph Mwangi", "joseph", "email address or a phone number"},
		{"Joseph Mwangi", "12345", "contact must be"},
	} {
		if _, _, err := validatePassRequest(tc.name, tc.contact); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%q/%q: want a refusal saying %q, got %v", tc.name, tc.contact, tc.says, err)
		}
	}

	// One pass per contact means one per person, not one per spelling.
	if normaliseContact("Joseph.Mwangi@Example.org") != normaliseContact(" joseph.mwangi@example.org ") {
		t.Error("the same address in different case or spacing would mint two passes")
	}
	if _, _, err := validatePassRequest("Joseph Mwangi", "joseph.mwangi@example.org"); err != nil {
		t.Errorf("an email contact was refused: %v", err)
	}
}

func TestAPassTokenIsStoredOnlyAsItsHash(t *testing.T) {
	tok, err := passToken()
	if err != nil {
		t.Fatal(err)
	}
	other, _ := passToken()
	if tok == other {
		t.Fatal("two tokens came out the same")
	}
	first, again := hashPassToken(tok), hashPassToken(string([]byte(tok)))
	if first != again || first == hashPassToken(other) {
		t.Error("the hash is not a stable function of the token alone")
	}
	if strings.Contains(hashPassToken(tok), tok) {
		t.Error("the stored form leaks the token")
	}
}

// The number is configuration; that there is a cap is not. A deployment that
// sets the cap to nothing gets the default and an error in its log, not an
// unlimited verifier.
func TestTheRateCapCanBeMovedButNotRemoved(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	rc := loadRateCap(log)
	if rc.Cap != defaultRateCap || rc.Window != defaultRateWindow {
		t.Fatalf("defaults: %+v", rc)
	}

	t.Setenv("CREST_VERIFY_RATE_CAP", "7")
	t.Setenv("CREST_VERIFY_RATE_WINDOW", "90s")
	rc = loadRateCap(log)
	if rc.Cap != 7 || rc.Window != 90*time.Second {
		t.Errorf("a configured cap was not read: %+v", rc)
	}

	for _, bad := range []string{"0", "-3", "lots"} {
		t.Setenv("CREST_VERIFY_RATE_CAP", bad)
		if rc = loadRateCap(log); rc.Cap != defaultRateCap {
			t.Errorf("CREST_VERIFY_RATE_CAP=%q gave cap %d, want the default %d", bad, rc.Cap, defaultRateCap)
		}
	}
	t.Setenv("CREST_VERIFY_RATE_WINDOW", "0s")
	if rc = loadRateCap(log); rc.Window != defaultRateWindow {
		t.Errorf("a zero window was accepted: %+v", rc)
	}
}
