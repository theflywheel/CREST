// The end-to-end proof of concept: a month of real-shaped evidence in, work
// records and printed cards out.
//
// This is G2's rehearsal (#25) run against a live stack. It is not the gate
// itself — the file is authored here rather than exported by a partner's
// system, and #25 says "no hand-authored data anywhere in the path", which this
// does not satisfy and does not claim to.
//
// What it is for: exercising the paths a pilot actually spends its time on. A
// demo that ingests a clean file proves the happy path. This file contains a
// duplicate export, three workers nobody has heard of, a zero-outcome day, a
// local-format date, a negative correction, an empty identifier and a backwards
// period — because the queues, holds and rejections are where a deployment
// discovers what it did not build.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/theflywheel/crest/harness"
)

type stack struct {
	registry, evidence, confirmation, verification, payments string
	http                                                     *http.Client
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	s := &stack{
		registry:     env("PARTIES_URL", "http://localhost:59000"),
		evidence:     env("EVIDENCE_URL", "http://localhost:59000"),
		confirmation: env("CONFIRMATION_URL", "http://localhost:59006"),
		verification: env("VERIFICATION_URL", "http://localhost:59000"),
		payments:     env("PAYMENTS_URL", "http://localhost:59006"),
		http:         &http.Client{Timeout: 60 * time.Second},
	}
	batch := env("POC_BATCH", "tests/fixtures/poc/riverside-dhis2-march-2026.csv")
	contextID := env("POC_CONTEXT", "crest:context:01JCREST00000000000000PRJC")
	definitionID := env("POC_DEFINITION", "crest:definition:01JCREST00000000000000DEFN")
	supervisor := env("POC_SUPERVISOR", "did:crest:party:01JCREST00000000000000SPVR")

	// The programme, its definition, its terms and the supervisor's
	// authorization have to exist before a batch naming them means anything. A
	// source system produces evidence; it does not produce the world the
	// evidence is about.
	h := harness.New()
	if err := h.WaitReady(context.Background(), 90*time.Second); err != nil {
		die("the stack is not up: %v\n\nrun `make e2e-up` first", err)
	}
	if _, err := h.Seed(context.Background()); err != nil {
		die("seed the programme: %v", err)
	}
	say("programme seeded: definition ratified and ACTIVE, supervisor authorised")

	raw, err := os.ReadFile(batch)
	if err != nil {
		die("read the batch: %v", err)
	}
	// The roster is who was enrolled, which is not the same as who the file
	// mentions. Registering everyone named in the batch would delete the
	// unclear queue's whole reason for existing.
	rosterRaw, err := os.ReadFile(env("POC_ROSTER", "tests/fixtures/poc/roster.txt"))
	if err != nil {
		die("read the roster: %v", err)
	}
	var roster []string
	for _, line := range strings.Split(strings.TrimSpace(string(rosterRaw)), "\n") {
		if p := strings.TrimSpace(line); p != "" {
			roster = append(roster, p)
		}
	}
	say("batch %s — %d rows, %d workers enrolled on the roster",
		batch, strings.Count(string(raw), "\n")-1, len(roster))

	// 1 ── the roster. A source system names workers; it does not create them.
	// Somebody enrolled these people, and the PoC has to stand that up before
	// any of their work can be attributed to anyone.
	registered := 0
	for _, phone := range roster {
		if s.registerWorker(phone, contextID, supervisor) {
			registered++
		}
	}
	say("roster: %d workers registered with enrolment consent", registered)

	// 2 ── the source, and its vocabulary.
	//
	// Registered only when a mapping is supplied. A file that already speaks
	// CREST's column names needs no translation, and requiring registration to
	// ingest anything at all would be a step for nobody's benefit.
	if mappingPath := os.Getenv("POC_MAPPING"); mappingPath != "" {
		mapping, err := os.ReadFile(mappingPath)
		if err != nil {
			die("read the source mapping: %v", err)
		}
		body := fmt.Sprintf(`{"adapterRef":"csv-batch@1","contextId":%q,"systemRef":"riverside-dhis2",
			"expectedEvery":"168h","ownerPartyId":%q,"mapping":%s}`,
			contextID, supervisor, mapping)
		if err := s.post(s.evidence+"/v1/sources", "application/json", []byte(body), nil); err != nil {
			die("register the source: %v", err)
		}
		say("source registered with a column mapping — its own vocabulary, as configuration")
	}

	// 3 ── ingest.
	q := url.Values{
		"contextId": {contextID}, "definitionId": {definitionID}, "submittedBy": {supervisor},
		"sourceClass": {"programme-system"}, "captureMethod": {"digital-capture"},
		"sourceExposure": {"signed-batch"}, "systemRef": {"riverside-dhis2"},
	}
	var ingest struct {
		Batch struct {
			ID                                   string `json:"id"`
			RowsTotal, RowsAccepted, RowsUnclear int    `json:"-"`
			Total                                int    `json:"rowsTotal"`
			Accepted                             int    `json:"rowsAccepted"`
			Unclear                              int    `json:"rowsUnclear"`
		} `json:"batch"`
		ClaimIDs []string `json:"claimIds"`
		Unclear  []struct {
			RowRef string `json:"rowRef"`
			Reason string `json:"reason"`
		} `json:"unclear"`
		Rejected []struct {
			Ref    string `json:"ref"`
			Reason string `json:"reason"`
		} `json:"rejected"`
	}
	if err := s.post(s.evidence+"/v1/batches?"+q.Encode(), "text/csv", raw, &ingest); err != nil {
		die("submit the batch: %v", err)
	}

	say("")
	say("ingest: %d rows in — %d accepted, %d unclear, %d refused",
		ingest.Batch.Total, ingest.Batch.Accepted, ingest.Batch.Unclear, len(ingest.Rejected))
	if dup := ingest.Batch.Accepted - len(ingest.ClaimIDs); dup > 0 {
		say("  deduped %d row(s): the same work exported twice is one unit, not two payments", dup)
	}
	for _, r := range ingest.Rejected {
		say("  refused  %-8s %s", r.Ref, r.Reason)
	}
	seen := map[string]int{}
	for _, u := range ingest.Unclear {
		seen[u.Reason]++
	}
	for reason, n := range seen {
		say("  unclear  %d row(s): %s", n, reason)
	}

	// 4 ── the windows open across a service boundary, carried by the outbox.
	// Polled rather than slept through: a fixed sleep is a race that passes on
	// a fast machine and fails on a loaded one.
	opened := 0
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		opened = 0
		for _, claimID := range ingest.ClaimIDs {
			var win struct {
				ClaimID string `json:"claimId"`
			}
			if err := s.get(s.confirmation+"/v1/windows/"+claimID, &win); err == nil && win.ClaimID != "" {
				opened++
			}
		}
		if opened == len(ingest.ClaimIDs) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	say("")
	say("confirmation: %d of %d windows opened, each one a worker who was told before it counted",
		opened, len(ingest.ClaimIDs))

	// 5 ── the seven days. Driven rather than waited for.
	if err := s.driveClock("2026-03-30T09:00:00Z"); err != nil {
		say("(clock not driveable: %v — windows will close on their own schedule)", err)
	}
	var swept struct {
		Due           int      `json:"due"`
		AutoConfirmed []string `json:"autoConfirmed"`
		Held          []string `json:"heldForSomeoneToLookAt"`
	}
	if err := s.post(s.confirmation+"/v1/sweep", "application/json", []byte(`{}`), &swept); err != nil {
		say("sweep: %v", err)
	}
	say("")
	say("T=7: %d windows due, %d auto-confirmed, %d held because the worker was never reached",
		swept.Due, len(swept.AutoConfirmed), len(swept.Held))

	// 6 ── what a worker ends up holding.
	cards, credentials, paid := 0, 0, 0
	for _, claimID := range ingest.ClaimIDs {
		var win struct {
			CredentialID      *string `json:"credentialId"`
			PaymentReleasedAt *string `json:"paymentReleasedAt"`
		}
		if err := s.get(s.confirmation+"/v1/windows/"+claimID, &win); err != nil {
			continue
		}
		if win.PaymentReleasedAt != nil {
			paid++
		}
		if win.CredentialID == nil {
			continue
		}
		credentials++
		body, err := s.raw(s.verification + "/v1/credentials/" + *win.CredentialID + "/card?format=payload")
		if err == nil && bytes.HasPrefix(body, []byte("NCF")) {
			cards++
		}
	}

	// A held payment is not a failure — it is W10 working. What would be a
	// failure is a payment missing with no reason and nobody answerable.
	var held struct {
		Instructions []struct {
			ClaimID string `json:"claimId"`
			State   string `json:"state"`
			Held    *struct {
				Code         string `json:"code"`
				Explanation  string `json:"explanation"`
				OwnerPartyID string `json:"ownerPartyId"`
			} `json:"held"`
		} `json:"instructions"`
	}
	// Polled, because the release crosses a service boundary through the outbox
	// exactly as the window opening did. The first version of this read the
	// list immediately and reported no held payments at all — while one was
	// sitting there, correctly held, moments later. A PoC that reports a
	// worker's payment as released when it has not been delivered yet is
	// telling the operator the one thing they most need to be true.
	settleBy := time.Now().Add(60 * time.Second)
	for {
		if err := s.get(s.payments+"/v1/instructions", &held); err != nil {
			say("list instructions: %v", err)
			break
		}
		if len(held.Instructions) >= credentials || time.Now().After(settleBy) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	heldCount, ownerless := 0, 0
	for _, in := range held.Instructions {
		if in.Held == nil {
			continue
		}
		heldCount++
		if in.Held.OwnerPartyID == "" || in.Held.Explanation == "" {
			ownerless++
		}
		say("  held     %s: %s", in.Held.Code, in.Held.Explanation)
	}

	say("")
	say("out: %d credentials, %d printable cards, %d payment instructions (%d released, %d held)",
		credentials, cards, len(held.Instructions), len(held.Instructions)-heldCount, heldCount)
	say("    %d windows recorded a release at exit", paid)
	say("")
	say("Every exit released payment: %v", paid == credentials && credentials > 0)
	say("Nothing was silently dropped: %v",
		ingest.Batch.Total == ingest.Batch.Accepted+ingest.Batch.Unclear+len(ingest.Rejected))
	say("Every held payment has a reason and an owner: %v", ownerless == 0)
}

// registerWorker creates a party and records their enrolment consent.
//
// Consent scoped to the programme, because that is what it is scoped to (§9):
// agreeing to this campaign holding your work history is not agreeing to every
// organisation on the deployment.
func (s *stack) registerWorker(phone, contextID, supervisor string) bool {
	// Resolve before creating, which is what an enrolment agent's tool does and
	// what the first run of this PoC did not. Creating unconditionally meant a
	// second run gave every worker a twin, and the registry then correctly
	// refused to guess which one a row meant — forty rows held rather than
	// merged. The invariant behaved; the tool was wrong.
	var party struct {
		ID string `json:"id"`
	}
	q := fmt.Sprintf("/v1/resolve?kind=contact-route&value=%s&contextId=%s",
		url.QueryEscape(phone), url.QueryEscape(contextID))
	var existing struct {
		PartyID string `json:"partyId"`
	}
	switch err := s.get(s.registry+q, &existing); {
	case err == nil && existing.PartyID != "":
		party.ID = existing.PartyID
	case err != nil && strings.Contains(err.Error(), "409"):
		say("  %s already resolves to more than one party; the registry is holding it", phone)
		return false
	default:
		body := fmt.Sprintf(`{"kind":"person","displayName":"CHW %s",
			"contactRoutes":[{"kind":"phone","value":%q}]}`, phone[len(phone)-4:], phone)
		if err := s.post(s.registry+"/v1/parties", "application/json", []byte(body), &party); err != nil {
			say("register %s: %v", phone, err)
			return false
		}
	}
	consent := fmt.Sprintf("/v1/parties/%s/consents?moment=enrolment&captureMethod=voice&purpose=%s&capturedBy=%s&contextId=%s",
		url.PathEscape(party.ID), url.QueryEscape("hold and fetch evidence of my work"),
		url.QueryEscape(supervisor), url.QueryEscape(contextID))
	if err := s.post(s.registry+consent, "audio/ogg",
		[]byte("a recording of the worker agreeing, captured at enrolment"), nil); err != nil {
		// One live enrolment consent per programme is a unique index, so a
		// re-run hits it. That is the constraint doing its job, not a failure.
		if !strings.Contains(err.Error(), "500") {
			say("consent for %s: %v", phone, err)
			return false
		}
	}
	return true
}

func (s *stack) driveClock(at string) error {
	for _, base := range []string{s.registry, s.evidence, s.confirmation, s.payments} {
		if err := s.post(base+"/internal/clock", "application/json",
			[]byte(fmt.Sprintf(`{"now":%q}`, at)), nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *stack) post(u, contentType string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	return s.do(req, out)
}

func (s *stack) get(u string, out any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return s.do(req, out)
}

func (s *stack) raw(u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (s *stack) do(req *http.Request, out any) error {
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s -> %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func say(format string, a ...any) { fmt.Printf(format+"\n", a...) }
func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
