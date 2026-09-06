package attestation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/service"
)

func TestReachCallbackRejectsUnverifiableOutcomeBeforeDatabaseAccess(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/internal/windows/claim-1/reach", nil)
	rr := httptest.NewRecorder()
	h := &windowHandlers{d: service.Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	h.reach(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid reach callback status = %d, want 400", rr.Code)
	}
}

func TestContestDecisionRejectsUnreasonedCorrection(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/contests/c-1/decide", strings.NewReader(`{"decision":"CORRECTED","reason":"","evidence":""}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h := &windowHandlers{d: service.Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	h.decideContest(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid contest decision status = %d, want 400", rr.Code)
	}
}

func TestReviewTokenIsBoundToStoredDigest(t *testing.T) {
	token := "one-time-token"
	digest := sha256.Sum256([]byte(token))
	hash := base64.RawURLEncoding.EncodeToString(digest[:])
	win := Window{reviewTokenHash: &hash, ClosesAt: time.Now()}
	if !validReviewToken(win, token) || validReviewToken(win, "other-token") {
		t.Fatal("review token digest check did not separate valid and invalid links")
	}
}

func TestAutoExitEligibilityRequiresCurrentDueReachedWindow(t *testing.T) {
	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	reached := "reached"
	unreached := "unreached"
	cases := []struct {
		name string
		win  Window
		want bool
	}{
		{name: "due and reached", win: Window{ClosesAt: now, Reach: &reached}, want: true},
		{name: "still open", win: Window{ClosesAt: now.Add(time.Second), Reach: &reached}},
		{name: "no delivery result", win: Window{ClosesAt: now.Add(-time.Second)}, want: false},
		{name: "failed delivery", win: Window{ClosesAt: now.Add(-time.Second), Reach: &unreached}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoExitEligible(tc.win, now); got != tc.want {
				t.Fatalf("autoExitEligible() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDisputeBodyFingerprintUsesExactWireBytes(t *testing.T) {
	raw := []byte(`{"reason":"same","raisedByPartyId":"p-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/claims/c-1/dispute", bytes.NewReader(raw))
	var body struct {
		Reason          string `json:"reason"`
		RaisedByPartyID string `json:"raisedByPartyId"`
	}
	got, ok := readDisputeJSON(httptest.NewRecorder(), req, &body)
	if !ok || !bytes.Equal(got, raw) {
		t.Fatalf("readDisputeJSON() did not preserve request bytes")
	}
	if body.Reason != "same" || body.RaisedByPartyID != "p-1" {
		t.Fatalf("decoded dispute = %#v", body)
	}
}

func TestDisputeRequiresIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/claims/c-1/dispute", nil)
	rr := httptest.NewRecorder()
	if _, ok := requireDisputeIdempotencyKey(rr, req); ok || rr.Code != http.StatusBadRequest {
		t.Fatalf("missing dispute key status = %d, ok=%v; want 400,false", rr.Code, ok)
	}
}

func TestWindowDetailsRejectsUnrelatedAuthenticatedParty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/claims/c-1/contest", nil)
	req = req.WithContext(identity.NewContext(req.Context(), identity.Caller{
		Subject: "subject", PartyID: "other",
	}))
	rr := httptest.NewRecorder()
	d := service.Deps{
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Permits: func(context.Context, string, string, string) (bool, error) { return false, nil },
	}
	if authorizeWindowDetails(rr, req, d, Window{PartyID: "worker", ContextID: "project"}) {
		t.Fatal("unrelated party was allowed to read contest details")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unrelated party status = %d, want 403", rr.Code)
	}
}

func TestNotificationFailureCannotDowngradeTerminalWindow(t *testing.T) {
	at := time.Now()
	if notificationFailureApplicable(Window{AcknowledgedAt: &at}) {
		t.Fatal("acknowledged window remained eligible for a failure downgrade")
	}
	if notificationFailureApplicable(Window{ExitRoute: stringPtr("self")}) {
		t.Fatal("exited window remained eligible for a failure downgrade")
	}
	if !notificationFailureApplicable(Window{}) {
		t.Fatal("open unacknowledged window was incorrectly treated as terminal")
	}
}

func TestLegacyAdoptionPreservesAcknowledgedAndClosedWindows(t *testing.T) {
	at := time.Now()
	hash := "stored-hash"
	if !legacyWindowNeedsReview(Window{}) {
		t.Fatal("open tokenless window was not selected for review adoption")
	}
	for _, win := range []Window{
		{AcknowledgedAt: &at},
		{ExitRoute: stringPtr("dispute")},
		{reviewTokenHash: &hash},
	} {
		if legacyWindowNeedsReview(win) {
			t.Fatalf("legacy adoption selected protected window: %#v", win)
		}
	}
}
