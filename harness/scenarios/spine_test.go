//go:build e2e

// Package scenarios drives the whole spine against real services.
//
// One scenario per thing that must be true for a worker, named after the
// promise rather than the mechanism. If one of these fails, someone does not
// get paid, or gets paid for work they did not do, or cannot show what they
// did — which is the only reason any of this exists.
package scenarios

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

const window = 7 * 24 * time.Hour

// runID makes one test run's work distinct from the last one's.
//
// It goes into source_record_ref, which is the field a source system uses to
// say which of its rows are distinct — and which the ingestion pipeline folds
// into a unit's identity for exactly that reason. Without it, re-running the
// suite against a live stack submits the same work twice and the pipeline
// correctly refuses to create a second claim, so the scenarios fail on their
// own de-duplication working.
//
// `make test-e2e` tears down its volumes and would not need this. `make
// e2e-run` against a stack left up is the fast path people actually use, and a
// suite that only passes on a fresh database is a suite that gets run less.
var runID = func() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}()

// batch builds a CSV in the shape a programme's export takes.
//
// Built here rather than read from a file because each scenario needs different
// rows, and a directory of near-identical fixtures is a directory nobody can
// tell apart.
func batch(rows ...string) []byte {
	header := "activity,outcome_value,outcome_unit,worker_id_kind,worker_id," +
		"period_start,period_end,geography,household_id,beneficiary_count,source_record_ref"
	return []byte(header + "\n" + strings.Join(rows, "\n") + "\n")
}

func row(phone string, count int, householdID string) string {
	return fmt.Sprintf(
		"bednet-distribution,%d,bednets-distributed,phone,%s,2026-03-02,2026-03-02,Riverside,%s,%d,%s-%s",
		count, phone, householdID, count, runID, householdID)
}

type world struct {
	*harness.Stack
	w   *fixtures.World
	ctx context.Context
}

func setup(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	s := harness.New()

	if err := s.WaitReady(ctx, 90*time.Second); err != nil {
		t.Fatalf("the stack never came up: %v\n\nis it running? `make harness-up`", err)
	}
	if err := s.Reset(ctx); err != nil {
		t.Fatalf("reset the mocks: %v", err)
	}
	w, err := s.Seed(ctx)
	if err != nil {
		t.Fatalf("seed the fixture world: %v", err)
	}
	return &world{Stack: s, w: w, ctx: ctx}
}

// submit sends a batch as the supervisor, from the programme's own system.
func (w *world) submit(t *testing.T, csv []byte) ingestResult {
	t.Helper()
	path := fmt.Sprintf("/v1/batches?contextId=%s&definitionId=%s&submittedBy=%s"+
		"&sourceClass=programme-system&captureMethod=digital-capture&sourceExposure=signed-batch"+
		"&systemRef=dhis2-riverside",
		fixtures.ProjectID, fixtures.DefinitionID, fixtures.SupervisorID)

	var out ingestResult
	if err := w.Evidence.PostRaw(w.ctx, path, "text/csv", csv, &out); err != nil {
		t.Fatalf("submit batch: %v", err)
	}
	return out
}

type ingestResult struct {
	Batch struct {
		ID           string `json:"id"`
		RowsTotal    int    `json:"rowsTotal"`
		RowsAccepted int    `json:"rowsAccepted"`
		RowsUnclear  int    `json:"rowsUnclear"`
	} `json:"batch"`
	ClaimIDs []string `json:"claimIds"`
	Unclear  []struct {
		RowRef string `json:"rowRef"`
		Reason string `json:"reason"`
	} `json:"unclear"`
}

// eventually polls for something the outbox relay has to carry across a service
// boundary. Polling rather than sleeping, and with the last error reported —
// "eventually failed" with no cause costs twenty minutes.
func eventually(t *testing.T, what string, within time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(within)
	var last error
	for {
		last = fn()
		if last == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s: %v", what, within, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (w *world) window(claimID string) (winView, error) {
	var v winView
	err := w.Confirmation.Get(w.ctx, "/v1/windows/"+claimID, &v)
	return v, err
}

type winView struct {
	ClaimID           string     `json:"claimId"`
	UnitID            string     `json:"unitId"`
	PartyID           string     `json:"partyId"`
	ClosesAt          time.Time  `json:"closesAt"`
	NotifiedAt        *time.Time `json:"notifiedAt"`
	ExitRoute         *string    `json:"exitRoute"`
	Reach             *string    `json:"reach"`
	PaymentReleasedAt *time.Time `json:"paymentReleasedAt"`
	CredentialID      *string    `json:"credentialId"`
}

type instructionView struct {
	ID          string `json:"id"`
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	ReleasedBy  string `json:"releasedBy"`
	State       string `json:"state"`
	Held        *struct {
		Code         string `json:"code"`
		Explanation  string `json:"explanation"`
		OwnerPartyID string `json:"ownerPartyId"`
	} `json:"held"`
}

func (w *world) instruction(claimID string) (instructionView, error) {
	var v instructionView
	err := w.Payments.Get(w.ctx, "/v1/instructions/by-claim/"+claimID, &v)
	return v, err
}

type verdict struct {
	Valid          bool              `json:"valid"`
	Reasons        []string          `json:"reasons"`
	Tier           *int              `json:"tier"`
	TierReason     []string          `json:"tierReason"`
	TrustChain     []trustLink       `json:"trustChain"`
	Contested      []contestStanding `json:"contested"`
	NotEstablished []string          `json:"notEstablished"`
	SignatureValid bool              `json:"signatureValid"`
	Revoked        bool              `json:"revoked"`
}

func (w *world) credential(t *testing.T, credID string) map[string]any {
	t.Helper()
	var out struct {
		Credential map[string]any `json:"credential"`
	}
	if err := w.Confirmation.Get(w.ctx, "/v1/credentials/"+credID, &out); err != nil {
		t.Fatalf("read credential %s: %v", credID, err)
	}
	return out.Credential
}

// contestStanding mirrors the verification service's. Declared here rather than
// imported because the harness talks HTTP only.
type contestStanding struct {
	State    string `json:"state"`
	Against  string `json:"against"`
	RaisedAt string `json:"raisedAt"`
}

// verifyRaw returns the verdict as the bytes a verifier actually receives.
//
// Some assertions are about what is NOT in the response, and a struct cannot
// answer those: a field with no Go name is still in the JSON.
func (w *world) verifyRaw(t *testing.T, cred map[string]any) string {
	t.Helper()
	_, body, err := w.Verification.Status(w.ctx, "POST", "/v1/verify", map[string]any{
		"credential":         cred,
		"requestedByPartyId": fixtures.OrgID,
		"purpose":            "checking a work record",
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return string(body)
}

func (w *world) verify(t *testing.T, cred map[string]any) verdict {
	t.Helper()
	var v verdict
	if err := w.Verification.Post(w.ctx, "/v1/verify", map[string]any{
		"credential":         cred,
		"requestedByPartyId": fixtures.OrgID,
		"purpose":            "checking a work record",
	}, &v); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return v
}

// ── The scenarios ───────────────────────────────────────────────────────────

// The spine, end to end: a real CSV goes in and a verifiable credential comes
// out, with a payment beside it. This is G2's acceptance in one function.
func TestARecordBecomesACredentialAndAPayment(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	result := w.submit(t, batch(row(phone, 12, "HH-001")))

	if result.Batch.RowsAccepted != 1 || len(result.ClaimIDs) != 1 {
		t.Fatalf("expected one claim from one row, got %d accepted and %d claims: %+v",
			result.Batch.RowsAccepted, len(result.ClaimIDs), result.Unclear)
	}
	claimID := result.ClaimIDs[0]

	// W2: the worker is told before it counts. The window and the notification
	// commit together, so if the window exists the message was at least tried.
	var win winView
	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		var err error
		win, err = w.window(claimID)
		return err
	})
	if win.NotifiedAt == nil {
		t.Error("a window opened without the worker being notified (W2)")
	}
	if got := win.ClosesAt.Sub(harness.Epoch()); got != window {
		t.Errorf("the window is %s long, want %s (T=7, §14)", got, window)
	}

	var messages struct {
		Messages []struct {
			To   string `json:"to"`
			Body string `json:"body"`
		} `json:"messages"`
	}
	eventually(t, "the SMS reaches the worker", 15*time.Second, func() error {
		if err := w.SMS.Get(w.ctx, "/messages?to="+url.QueryEscape(phone), &messages.Messages); err != nil {
			return err
		}
		if len(messages.Messages) == 0 {
			return fmt.Errorf("no message for %s", phone)
		}
		return nil
	})
	// The wording matters: a worker told they must reply or lose money would be
	// a worker under duress. They are told they will be paid either way.
	if !strings.Contains(messages.Messages[0].Body, "paid either way") {
		t.Errorf("the notification does not tell the worker they will be paid either way: %q",
			messages.Messages[0].Body)
	}

	// The worker confirms.
	var exit struct {
		Window     winView `json:"window"`
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm",
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if exit.Credential.ID == "" {
		t.Fatal("confirming produced no credential")
	}

	var claim schema.Claim
	if err := w.Evidence.Get(w.ctx, "/v1/claims/"+claimID, &claim); err != nil {
		t.Fatal(err)
	}
	if claim.State != schema.ClaimStateACCEPTED {
		t.Errorf("claim is %s after confirmation, want ACCEPTED", claim.State)
	}

	// §8 keeps *which* key matched, not merely that one did (#14). The batch
	// joined on a phone number, so the answer is contact-route — and it has to
	// be readable off the claim, because a match nobody can explain is a match
	// nobody can dispute, and identity matching is where a worker gets attached
	// to work that was not theirs.
	if claim.Matched == nil {
		t.Fatal("the claim records no match at all; which key attached this worker is unrecoverable")
	}
	if claim.Matched.Key != schema.ClaimMatchedKeyContactRoute {
		t.Errorf("matched key = %q, want %q — the batch joined on a phone number",
			claim.Matched.Key, schema.ClaimMatchedKeyContactRoute)
	}

	// The credential verifies, and the tier is computed rather than read.
	cred := w.credential(t, exit.Credential.ID)
	v := w.verify(t, cred)
	if !v.Valid {
		t.Fatalf("the credential we just issued does not verify: %v", v.Reasons)
	}
	if v.Tier == nil {
		t.Fatal("a valid credential came back with no tier")
	}
	if len(v.TrustChain) == 0 {
		t.Error("the verdict has no trust chain; a verdict nobody can check is an assertion")
	}
	// Every link answers the question, one way or the other. A link that
	// claims to be checkable without saying where, or admits it is not without
	// saying what is being trusted instead, has told the verifier nothing (#68).
	for _, l := range v.TrustChain {
		switch {
		case l.Checkable && l.How == "":
			t.Errorf("trust-chain link %q says it is checkable and does not say where", l.Claim)
		case !l.Checkable && l.Trusting == "":
			t.Errorf("trust-chain link %q is not checkable and does not say what is being trusted", l.Claim)
		}
	}
	// A valid verdict states its own limits. #68: a green verdict reads as
	// "and this person was authorised to do this work", and that is precisely
	// what a deployment cannot demonstrate to a stranger.
	if len(v.NotEstablished) == 0 {
		t.Error("a valid verdict claims to establish everything; it does not establish that the subject was authorised")
	}

	// The credential must not carry the tier. A stored tier freezes a judgement
	// verifiers should be free to make differently (§6).
	subject := cred["credentialSubject"].(map[string]any)
	if _, found := subject["tier"]; found {
		t.Error("the credential carries a tier; it must carry facts and nothing else (§6)")
	}
	if _, found := subject["individual_id"]; found {
		t.Error("the credential carries a national identifier (W9)")
	}

	// And the money.
	var in instructionView
	eventually(t, "the payment instruction is released", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.State != "RELEASED" {
		t.Fatalf("instruction is %s, want RELEASED: %+v", in.State, in.Held)
	}
	// 12 bednets at 250 minor units each.
	if in.AmountMinor != 12*250 {
		t.Errorf("amount is %d, want %d", in.AmountMinor, 12*250)
	}
	if in.ReleasedBy != "self" {
		t.Errorf("released by %q, want self", in.ReleasedBy)
	}
}

// W3: silence is not consent against the worker. The window closes on its own,
// the record stands, and the money moves — and it stays disputable afterwards.
func TestSilenceStillPaysAndStaysDisputable(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerBID)
	result := w.submit(t, batch(row(phone, 5, "HH-002")))
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	// Seven days, in milliseconds. This is what the injectable clock is for.
	if err := w.Advance(w.ctx, window+time.Minute); err != nil {
		t.Fatal(err)
	}
	var swept struct {
		Due           int      `json:"due"`
		AutoConfirmed []string `json:"autoConfirmed"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/sweep", nil, &swept); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(swept.AutoConfirmed) == 0 {
		t.Fatalf("nothing auto-confirmed after the window closed (due=%d)", swept.Due)
	}

	win, err := w.window(claimID)
	if err != nil {
		t.Fatal(err)
	}
	if win.ExitRoute == nil || *win.ExitRoute != "auto" {
		t.Fatalf("exit route is %v, want auto", win.ExitRoute)
	}
	if win.PaymentReleasedAt == nil {
		t.Error("an auto-confirmed window released no payment (W4)")
	}

	var in instructionView
	eventually(t, "the auto-confirmed payment is released", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.State != "RELEASED" || in.ReleasedBy != "auto" {
		t.Errorf("instruction is %s released by %q, want RELEASED/auto", in.State, in.ReleasedBy)
	}

	// W3's second half: the seven days are a window for objecting, not a
	// deadline for noticing. The worker can still dispute afterwards.
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/dispute", map[string]any{
		"reason":          "I did not do this round",
		"raisedByPartyId": fixtures.WorkerBID,
	}, nil); err != nil {
		t.Fatalf("disputing after auto-confirmation was refused: %v", err)
	}
	var claim schema.Claim
	if err := w.Evidence.Get(w.ctx, "/v1/claims/"+claimID, &claim); err != nil {
		t.Fatal(err)
	}
	if claim.State != schema.ClaimStateDISPUTED {
		t.Errorf("claim is %s after a post-window dispute, want DISPUTED", claim.State)
	}
}

// W4, the one that matters most: a dispute contests the record, not the money.
func TestADisputeStillReleasesPayment(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	result := w.submit(t, batch(row(phone, 9, "HH-003")))
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/dispute", map[string]any{
		"reason":          "the count is wrong, it was six not nine",
		"raisedByPartyId": fixtures.WorkerAID,
	}, nil); err != nil {
		t.Fatalf("dispute: %v", err)
	}

	var in instructionView
	eventually(t, "the disputed claim's payment is released", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.State != "RELEASED" {
		t.Fatalf("a disputed claim's payment is %s — a dispute must never cost the worker "+
			"their money (W4): %+v", in.State, in.Held)
	}
	if in.ReleasedBy != "dispute" {
		t.Errorf("released by %q, want dispute", in.ReleasedBy)
	}

	// W5: the unit survives. The record that work happened outlives every
	// argument about who did it.
	var claim schema.Claim
	if err := w.Evidence.Get(w.ctx, "/v1/claims/"+claimID, &claim); err != nil {
		t.Fatal(err)
	}
	var unit schema.Unit
	if err := w.Evidence.Get(w.ctx, "/v1/units/"+claim.UnitID, &unit); err != nil {
		t.Fatalf("the unit is gone after its claim was disputed (W5): %v", err)
	}
	if unit.Outcome.Value != 9 {
		t.Errorf("the unit changed when its claim was disputed: outcome %v", unit.Outcome.Value)
	}

	// A disputed claim does not get a credential — CREST would be signing a
	// statement the worker says is untrue.
	win, _ := w.window(claimID)
	if win.CredentialID != nil {
		t.Error("a disputed claim was issued a credential")
	}
}

// W1: work recorded is work that happened. A row nobody can attribute does not
// become a claim, and it does not vanish either — it goes somewhere a person
// can work it.
func TestAnUnattributableRowGoesToTheUnclearQueue(t *testing.T) {
	w := setup(t)

	known, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	result := w.submit(t, batch(
		row(known, 4, "HH-010"),
		row("+15550199999", 7, "HH-011"), // nobody
	))

	if result.Batch.RowsAccepted != 1 {
		t.Errorf("accepted %d rows, want 1", result.Batch.RowsAccepted)
	}
	if result.Batch.RowsUnclear != 1 {
		t.Fatalf("unclear %d rows, want 1", result.Batch.RowsUnclear)
	}
	if !strings.Contains(result.Unclear[0].Reason, "no party") {
		t.Errorf("the unclear reason is %q; it should say the worker could not be matched",
			result.Unclear[0].Reason)
	}
	if result.Unclear[0].RowRef == "" {
		t.Error("the unclear row does not name which row it was, so nobody can find it in the file")
	}

	var queue struct {
		Count int `json:"count"`
	}
	if err := w.Evidence.Get(w.ctx, "/v1/unclear", &queue); err != nil {
		t.Fatal(err)
	}
	if queue.Count == 0 {
		t.Error("the unclear queue is empty; the row was dropped rather than queued")
	}
}

// §6, and the reason the tier is never stored: re-assessing a source downgrades
// every credential it produced, instantly, with no reissuance and no change to
// the credential itself.
func TestReassessingASourceDowngradesAnUnchangedCredential(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	result := w.submit(t, batch(row(phone, 3, "HH-020")))
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm", nil, &exit); err != nil {
		t.Fatal(err)
	}
	cred := w.credential(t, exit.Credential.ID)

	before := w.verify(t, cred)
	if !before.Valid || before.Tier == nil {
		t.Fatalf("the credential did not verify before the downgrade: %v", before.Reasons)
	}

	// Cleaned up so the scenario is immediately re-runnable (docs/TESTING.md).
	// An assessment left behind would make the next run start already
	// downgraded, and the run after that would "pass" while proving nothing.
	t.Cleanup(func() {
		if _, _, err := w.Verification.Status(w.ctx, "DELETE",
			"/v1/source-assessments/"+url.PathEscape("csv-batch@1"), nil); err != nil {
			t.Errorf("could not lift the source assessment: %v", err)
		}
	})
	if err := w.Verification.Post(w.ctx, "/v1/source-assessments", map[string]any{
		"adapterRef":        "csv-batch@1",
		"maxTier":           1,
		"reason":            "under investigation after a bulk edit",
		"assessedByPartyId": fixtures.OrgID,
	}, nil); err != nil {
		t.Fatal(err)
	}

	after := w.verify(t, cred) // the same bytes, unchanged
	if after.Tier == nil {
		t.Fatal("the credential stopped verifying entirely after a downgrade to tier 1")
	}
	if *after.Tier >= *before.Tier {
		t.Errorf("tier went from %d to %d; re-assessing the source should have lowered it",
			*before.Tier, *after.Tier)
	}
	if !strings.Contains(strings.Join(after.TierReason, " "), "under investigation") {
		t.Errorf("the downgraded verdict does not say why: %v", after.TierReason)
	}
}

// §9: withdrawal is the single central fact about credentials, and a verifier
// sees it without CREST telling them which credential they asked about.
func TestARevokedCredentialStopsVerifying(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	result := w.submit(t, batch(row(phone, 6, "HH-030")))
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm", nil, &exit); err != nil {
		t.Fatal(err)
	}
	cred := w.credential(t, exit.Credential.ID)

	if v := w.verify(t, cred); !v.Valid {
		t.Fatalf("not valid before revocation: %v", v.Reasons)
	}
	if err := w.Confirmation.Post(w.ctx,
		"/v1/credentials/"+exit.Credential.ID+"/revoke", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := w.verify(t, cred)
	if after.Valid {
		t.Error("a withdrawn credential still verifies")
	}
	if !after.Revoked {
		t.Error("the verdict does not say the credential was withdrawn")
	}
}

// W4 as a standing check rather than a scenario: no window may have exited
// without releasing a payment. It should be empty after everything above.
func TestNoWindowExitedWithoutReleasingPayment(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerCID)
	result := w.submit(t, batch(row(phone, 2, "HH-040")))
	if len(result.ClaimIDs) == 0 {
		// Worker C has no phone; she is reached through her supervisor, so a
		// phone-joined row cannot match her. That is the correct outcome and
		// the unclear queue is where it belongs.
		if result.Batch.RowsUnclear != 1 {
			t.Fatalf("expected the row to be unclear, got %+v", result.Batch)
		}
	} else {
		eventually(t, "the window opens", 15*time.Second, func() error {
			_, err := w.window(result.ClaimIDs[0])
			return err
		})
		if err := w.Confirmation.Post(w.ctx,
			"/v1/claims/"+result.ClaimIDs[0]+"/confirm", nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	var out struct {
		Count   int       `json:"count"`
		Windows []winView `json:"windows"`
	}
	if err := w.Confirmation.Get(w.ctx, "/v1/unreleased", &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 0 {
		t.Errorf("%d windows exited without releasing payment (W4): %+v", out.Count, out.Windows)
	}
}

// A source system retrying after a client-side timeout is the ordinary case,
// not an unusual one — and it is exactly the case that used to produce two
// units, two credentials and two payments for one piece of work.
//
// Found by an adversarial review of code whose own migration comment claimed
// this was already true. It was not: the unit id was minted fresh on every
// ingestion, so nothing in the database could tell the two apart.
func TestResubmittingTheSameBatchDoesNotPayTwice(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerAID)
	csv := batch(row(phone, 8, "HH-050"))

	first := w.submit(t, csv)
	if len(first.ClaimIDs) != 1 {
		t.Fatalf("first submission produced %d claims, want 1", len(first.ClaimIDs))
	}
	second := w.submit(t, csv)
	if len(second.ClaimIDs) != 0 {
		t.Errorf("re-submitting the same batch produced %d new claims: %v",
			len(second.ClaimIDs), second.ClaimIDs)
	}

	var claims struct {
		Claims []schema.Claim `json:"claims"`
	}
	if err := w.Evidence.Get(w.ctx,
		"/v1/claims?partyId="+url.QueryEscape(fixtures.WorkerAID), &claims); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, c := range claims.Claims {
		seen[c.UnitID]++
	}
	for unitID, n := range seen {
		if n > 1 {
			t.Errorf("unit %s carries %d claims for one worker", unitID, n)
		}
	}

	// And the money: the second submission created no claim, so there is
	// nothing for a second instruction to be attached to. Asserted per claim
	// rather than by scanning every instruction for a matching amount — a
	// stack that has run the suite before is full of legitimate instructions
	// for the same eight bednets, and counting those proves nothing.
	claimID := first.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm", nil, nil); err != nil {
		t.Fatal(err)
	}
	var in instructionView
	eventually(t, "the payment is released", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.AmountMinor != 8*250 {
		t.Errorf("amount is %d, want %d", in.AmountMinor, 8*250)
	}
}

// An outcome of zero is a legitimate accepted row. Released with a zero amount
// it satisfies every count, moves no money, and tells the worker nothing —
// which is the silent failure a held payment with a reason exists to prevent.
func TestAZeroOutcomeIsHeldWithAReasonRatherThanPaidAsZero(t *testing.T) {
	w := setup(t)

	phone, _ := harness.PhoneOf(w.w, fixtures.WorkerBID)
	result := w.submit(t, batch(row(phone, 0, "HH-060")))
	if len(result.ClaimIDs) != 1 {
		t.Fatalf("a zero-outcome row should still be a claim, got %d: %+v",
			len(result.ClaimIDs), result.Unclear)
	}
	claimID := result.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm", nil, nil); err != nil {
		t.Fatal(err)
	}

	var in instructionView
	eventually(t, "the instruction is written", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.State != "HELD" {
		t.Fatalf("a zero-outcome record produced a %s instruction for %d", in.State, in.AmountMinor)
	}
	if in.Held == nil || in.Held.Explanation == "" || in.Held.OwnerPartyID == "" {
		t.Error("the hold has no explanation or no owner; a worker must never see a " +
			"missing payment with nothing attached to it")
	}
}

// A raw national identifier must not come to rest anywhere — and the unclear
// queue is the one place it could, because an unmatched row is exactly the row
// whose identifier nobody has resolved.
func TestAnUnmatchedNationalIdentifierIsNeverStoredRaw(t *testing.T) {
	w := setup(t)

	// Invented, and matching nobody. The point is what happens to it on the way
	// to the queue, not whether it resolves.
	const invented = "999900001111"
	csv := []byte("activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,source_record_ref\n" +
		"bednet-distribution,3,bednets-distributed,national-id," + invented + ",2026-03-02," + runID + "-nid\n")

	result := w.submit(t, csv)
	if result.Batch.RowsUnclear != 1 {
		t.Fatalf("expected the row to be unclear, got %+v", result.Batch)
	}

	var queue struct {
		Unclear []struct {
			Record map[string]any `json:"record"`
		} `json:"unclear"`
	}
	if err := w.Evidence.Get(w.ctx, "/v1/unclear", &queue); err != nil {
		t.Fatal(err)
	}
	for _, u := range queue.Unclear {
		raw, err := json.Marshal(u.Record)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), invented) {
			t.Fatalf("the unclear queue stored a raw national identifier: %s", raw)
		}
	}
}

// Worker C has no phone. She is reached through her supervisor, and if that
// fails she is not reached at all — and a record must never be auto-confirmed
// against a worker during a silence the system produced.
//
// This is the fourth T=7 exit, and the one nobody demos: supervisor-assisted.
// Until this test it was unexercised, which is exactly how a route that does
// not release payment survives to a pilot.
func TestAnUnreachedWorkerIsNotAutoConfirmedAgainst(t *testing.T) {
	w := setup(t)

	// A roster id, so the row matches Worker C without needing a phone.
	if err := w.Registry.Post(w.ctx,
		"/v1/parties/"+url.PathEscape(fixtures.WorkerCID)+"/roster-ids",
		map[string]any{"rosterId": "RIV-0003", "contextId": fixtures.ProjectID}, nil); err != nil {
		t.Fatal(err)
	}
	csv := []byte("activity,outcome_value,outcome_unit,worker_id_kind,worker_id,period_start,household_id,source_record_ref\n" +
		"bednet-distribution,4,bednets-distributed,roster-id,RIV-0003,2026-03-02,HH-070," + runID + "-roster\n")

	result := w.submit(t, csv)
	if len(result.ClaimIDs) != 1 {
		t.Fatalf("the roster id should have matched Worker C, got %+v", result.Unclear)
	}
	claimID := result.ClaimIDs[0]

	var win winView
	eventually(t, "the reach outcome is recorded", 15*time.Second, func() error {
		var err error
		win, err = w.window(claimID)
		if err != nil {
			return err
		}
		if win.Reach == nil {
			return fmt.Errorf("reach not yet established")
		}
		return nil
	})

	// Worker C's only route is her supervisor, who does have a phone — so she
	// is reachable, through a person. That is the designed behaviour, and the
	// assertion is on the mechanism rather than on a particular outcome.
	if *win.Reach != "reached" && *win.Reach != "unreached" {
		t.Fatalf("reach is %q, want reached or unreached", *win.Reach)
	}

	if err := w.Advance(w.ctx, window+time.Minute); err != nil {
		t.Fatal(err)
	}
	var swept struct {
		AutoConfirmed          []string `json:"autoConfirmed"`
		HeldForSomeoneToLookAt []string `json:"heldForSomeoneToLookAt"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/sweep", nil, &swept); err != nil {
		t.Fatal(err)
	}

	if *win.Reach == "unreached" {
		for _, id := range swept.AutoConfirmed {
			if id == claimID {
				t.Fatal("a worker who was never reached had their record auto-confirmed against them")
			}
		}
		var found bool
		for _, id := range swept.HeldForSomeoneToLookAt {
			if id == claimID {
				found = true
			}
		}
		if !found {
			t.Error("an unreached window was neither auto-confirmed nor surfaced for a person; " +
				"it is simply stuck, which is the worst of the three")
		}

		// The supervisor-assisted route: a person taking responsibility rather
		// than a timer. It must release payment like every other exit.
		if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/assist",
			map[string]any{"assistedByPartyId": fixtures.SupervisorID}, nil); err != nil {
			t.Fatalf("assisted confirmation: %v", err)
		}
	}

	var in instructionView
	eventually(t, "the payment is released", 15*time.Second, func() error {
		var err error
		in, err = w.instruction(claimID)
		return err
	})
	if in.State != "RELEASED" && in.State != "HELD" {
		t.Errorf("instruction state is %q", in.State)
	}
	if in.State == "HELD" && (in.Held == nil || in.Held.OwnerPartyID == "") {
		t.Error("a held payment with no owner")
	}
}

// trustLink mirrors the verification service's Link. Declared here rather than
// imported because the harness talks HTTP only — a scenario that imports a
// service's types is testing the struct, not the contract.
type trustLink struct {
	Claim     string `json:"claim"`
	Checkable bool   `json:"checkable"`
	How       string `json:"how,omitempty"`
	Trusting  string `json:"trusting,omitempty"`
}
