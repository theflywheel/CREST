//go:build e2e

package scenarios

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/harness"
	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/schema"
)

// Caller identity in the scenarios (#89).
//
// Every scenario that acts in somebody's name now says whose name. That reads
// as ceremony until you look at what it replaced: before this, a request that
// confirmed a worker's week and a request that confirmed a stranger's week were
// the same bytes, and the record could not tell them apart afterwards.
//
// The helpers here are deliberately thin. `login` binds a subject and mints a
// token through the real endpoints, and `assist` is a supervisor's token plus
// the header naming who they are acting for — there is no shortcut that skips
// the binding or the signature, because a suite with one would be proving that
// the shortcut works.

// login authenticates as a party, binding a subject to them if it is not bound
// already.
//
// The provider subject is scoped to runID for the same reason source record
// refs are: the suite has to pass twice against one stack. A fresh runID means
// a fresh subject, appended to the party's bindings beside the last run's —
// which is exactly what identityBindings promises to do and now finally does.
func (w *world) login(t *testing.T, partyID string) harness.Caller {
	t.Helper()

	// Not cached across scenarios, and that is not an oversight. Every
	// setup(t) re-seeds the fixture world, and seeding a party POSTs the whole
	// Party document — which rebuilds its keys from that document and so drops
	// any binding appended since. A cached token then points at a subject
	// nothing is bound to any more, and every request from it comes back
	// "subject_not_enrolled", which reads as an authentication defect rather
	// than as the harness having reset the thing it was testing.
	//
	// Binding again is cheap and idempotent: appendBinding returns the party
	// unchanged when the same provider, class and subject are already there.
	providerSub := fmt.Sprintf("%s|%s", runID, strings.TrimPrefix(partyID, "did:crest:party:"))
	subject := w.oidc.Subject(providerSub)

	// Token first, then bind WITH it (#102). The binding endpoint's self-proof
	// is possession of the token whose subject is being bound — the one proof
	// that needs no prior binding, which is what makes first login possible at
	// all. Still bound through the real endpoint, so the assurance upgrade in
	// #17 is exercised on every run: a party that logs in holds a live
	// generic-oidc binding, which is IA-3.
	token, err := w.oidc.Token(w.ctx, providerSub)
	if err != nil {
		t.Fatalf("mint a token for %s: %v", partyID, err)
	}
	binding := map[string]any{
		"provider":      "mock-oidc",
		"providerClass": "generic-oidc",
		"subjectRef":    subject,
	}
	err = w.Parties.As(harness.Caller{Token: token}).
		Post(w.ctx, "/v1/parties/"+partyID+"/identity-bindings", binding, nil)
	if err == nil {
		return harness.Caller{Token: token}
	}

	// Self-proof did not carry, and for the fixture workers it is not meant
	// to. Anaya and Bina arrive already bound to an eSignet and an OTP
	// subject, and holding a valid token proves who the caller is, not that
	// they are the party in the URL — so claiming a party bound to somebody
	// else is exactly the thing the endpoint refuses.
	//
	// The door for that case is the one a real enrolment uses: an agent with
	// act-for-party binds on the worker's behalf. The supervisor holds that
	// grant on the project in the fixture world, so the suite goes through it
	// rather than around it. The supervisor is excluded because she is the
	// assistance — recursing to fetch her would not terminate.
	if partyID == fixtures.SupervisorID {
		t.Fatalf("bind %s to a subject: %v", partyID, err)
	}
	sup := w.login(t, fixtures.SupervisorID)
	sup.OnBehalfOf = partyID
	if aErr := w.Parties.As(sup).Post(w.ctx,
		"/v1/parties/"+partyID+"/identity-bindings?contextId="+fixtures.ProjectID,
		binding, nil); aErr != nil {
		t.Fatalf("bind %s to a subject: self-proof %v; assisted by the supervisor %v",
			partyID, err, aErr)
	}
	return harness.Caller{Token: token}
}

// assist is one party acting for another: the supervisor-assisted paths.
//
// It is a Caller like any other, and that is the point. The assisted case is
// not a special endpoint or a flag on a body — it is the same request with a
// header saying who it is for, refused unless the caller holds act-for-party in
// the context. Nothing about the worker's record changes shape because somebody
// helped them; only the route recorded against it does.
func (w *world) assist(t *testing.T, actor, forParty string) harness.Caller {
	t.Helper()
	c := w.login(t, actor)
	c.OnBehalfOf = forParty
	return c
}

// consentOf records an enrolment consent for a party and returns its id.
//
// Recorded rather than looked up, because the fixture world's consents are
// shared and a scenario that withdraws one takes it away from every scenario
// that runs after it — which is exactly the isolation bug that made the suite
// pass only on a fresh database once already.
func (w *world) consentOf(t *testing.T, party string) string {
	t.Helper()
	var consent struct {
		ID string `json:"id"`
	}
	if err := w.Parties.As(w.assist(t, fixtures.SupervisorID, party)).PostRaw(w.ctx, fmt.Sprintf(
		"/v1/parties/%s/consents?moment=enrolment&captureMethod=voice&purpose=%s&capturedBy=%s&contextId=%s",
		party, url.QueryEscape("hold and fetch evidence of my work"),
		url.QueryEscape(fixtures.SupervisorID), url.QueryEscape(fixtures.ProjectID)),
		"audio/ogg", []byte("a recording of the worker agreeing"), &consent); err != nil {
		t.Fatalf("record consent for %s: %v", party, err)
	}
	return consent.ID
}

// consentState reads where a party's enrolment consent stands.
func (w *world) consentState(t *testing.T, party string) string {
	t.Helper()
	var out struct {
		State string `json:"enrolmentConsent"`
	}
	// Scoped to the project, because consent is per programme and there is no
	// single answer about a worker without naming one (§9).
	if err := w.Parties.As(w.assist(t, fixtures.SupervisorID, party)).Get(w.ctx,
		"/v1/parties/"+party+"/enrolment-consent?contextId="+
			url.QueryEscape(fixtures.ProjectID), &out); err != nil {
		t.Fatalf("read enrolment consent for %s: %v", party, err)
	}
	return out.State
}

// newWorker creates a person reachable only through their supervisor, for the
// scenarios that are about who is calling rather than about whose work a row
// is. Fresh each time: a scenario that withdraws a fixture worker's consent
// takes it away from every scenario after it.
func (w *world) newWorker(t *testing.T, name string) string {
	t.Helper()
	var created schema.Party
	if err := w.Parties.Post(w.ctx, "/v1/parties", schema.Party{
		Kind:        schema.PartyKindPerson,
		DisplayName: name,
		// Reachable through their supervisor and by no other route. That is
		// the shape of the worker these scenarios are about — somebody with no
		// phone of their own, for whom the assisted paths are the only paths —
		// and the schema has a kind for exactly that, so it does not need a
		// phone number invented for them.
		ContactRoutes: []schema.PartyContactRoutesItem{{
			Kind:  schema.PartyContactRoutesItemKindSupervisor,
			Value: fixtures.SupervisorID,
		}},
	}, &created); err != nil {
		t.Fatalf("create worker: %v", err)
	}
	return created.ID
}

func TestAnUnauthenticatedCallerCannotWithdrawAWorkersConsent(t *testing.T) {
	w := setup(t)

	// This is the scenario #89 opens with, and the reason it was worth its own
	// issue: withdrawing enrolment consent stops new evidence being recorded
	// about a worker (§9). One call, no token, and somebody's work quietly
	// stops counting — with nothing in the record saying who did it.
	worker := w.newWorker(t, "Unprotected Worker "+runID)
	consentID := w.consentOf(t, worker)

	code, body, err := w.Parties.Status(w.ctx, http.MethodPost,
		"/v1/consents/"+consentID+"/withdraw", map[string]any{"reason": "not mine to give"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated withdrawal was answered %d, not 401: %s", code, body)
	}

	// And the consent is untouched. A refusal that still had the side effect
	// would be the worst of both.
	if state := w.consentState(t, worker); state != "GRANTED" {
		t.Fatalf("consent is %q after a refused withdrawal, not GRANTED", state)
	}
}

func TestOneWorkerCannotWithdrawAnothersConsent(t *testing.T) {
	w := setup(t)

	worker := w.newWorker(t, "Target Worker "+runID)
	consentID := w.consentOf(t, worker)

	// Authenticated, and authenticated as somebody else. This is the case the
	// old code could not see at all: the request was well formed, the consent
	// id was real, and there was nothing to compare the caller against.
	code, body, err := w.Parties.As(w.login(t, fixtures.WorkerBID)).
		Status(w.ctx, http.MethodPost, "/v1/consents/"+consentID+"/withdraw",
			map[string]any{"reason": "not mine to give"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("withdrawing another worker's consent was answered %d, not 403: %s", code, body)
	}
	if state := w.consentState(t, worker); state != "GRANTED" {
		t.Fatalf("consent is %q after a refused withdrawal, not GRANTED", state)
	}
}

func TestAWorkerWithdrawsTheirOwnConsent(t *testing.T) {
	w := setup(t)

	// The path that has to keep working. A control that refuses everything is
	// not a control, it is an outage, and the only way to tell them apart is a
	// scenario that proves the legitimate caller still gets through.
	worker := w.newWorker(t, "Consent Withdrawer "+runID)
	consentID := w.consentOf(t, worker)

	if err := w.Parties.As(w.login(t, worker)).Post(w.ctx,
		"/v1/consents/"+consentID+"/withdraw",
		map[string]any{"reason": "changed my mind"}, nil); err != nil {
		t.Fatalf("a worker could not withdraw their own consent: %v", err)
	}
	if state := w.consentState(t, worker); state != "WITHDRAWN" {
		t.Fatalf("consent state is %q, not WITHDRAWN", state)
	}
}

func TestASupervisorWithdrawsForAWorkerWhoCannotDoItThemselves(t *testing.T) {
	w := setup(t)

	// The assisted case, which is a requirement rather than a loophole: a
	// worker with no phone still has to be able to take their consent back, and
	// the only route they have is somebody doing it for them (§9). What makes
	// it safe is that it is written down — the supervisor's identity is on the
	// request, and they hold act-for-party in this project and nowhere else.
	worker := w.newWorker(t, "Assisted Withdrawer "+runID)
	consentID := w.consentOf(t, worker)

	if err := w.Parties.As(w.assist(t, fixtures.SupervisorID, worker)).Post(w.ctx,
		"/v1/consents/"+consentID+"/withdraw",
		map[string]any{"reason": "asked me to, has no phone"}, nil); err != nil {
		t.Fatalf("a supervisor could not withdraw for a worker: %v", err)
	}
	if state := w.consentState(t, worker); state != "WITHDRAWN" {
		t.Fatalf("consent state is %q, not WITHDRAWN", state)
	}
}

func TestAWorkerCannotActForAnotherWorkerJustByAsking(t *testing.T) {
	w := setup(t)

	// The header is not the permission. Worker B holds no act-for-party
	// authorization, so naming somebody else in it changes nothing except which
	// refusal they get.
	worker := w.newWorker(t, "Not Yours "+runID)
	consentID := w.consentOf(t, worker)

	caller := w.login(t, fixtures.WorkerBID)
	caller.OnBehalfOf = worker
	code, body, err := w.Parties.As(caller).Status(w.ctx, http.MethodPost,
		"/v1/consents/"+consentID+"/withdraw", map[string]any{"reason": "helping"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("acting for a worker without the authorization was answered %d, not 403: %s", code, body)
	}
	if state := w.consentState(t, worker); state != "GRANTED" {
		t.Fatalf("consent is %q after a refused withdrawal, not GRANTED", state)
	}
}

// refusedToken drives one enforced endpoint with a token that should not be
// accepted, and returns what happened.
//
// The consent id is deliberately one that does not exist. A token is rejected
// by the middleware before any handler runs, so a scenario about a bad token
// needs no fixture to point at — and one that used a real consent would be
// unable to tell "the token was refused" from "the consent was not found".
func (w *world) refusedToken(t *testing.T, token string) (int, []byte) {
	t.Helper()
	code, body, err := w.Parties.As(harness.Caller{Token: token}).
		Status(w.ctx, http.MethodPost, "/v1/consents/crest:consent:does-not-exist/withdraw",
			map[string]any{"reason": "no"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	return code, body
}

func TestATamperedTokenIsRefused(t *testing.T) {
	w := setup(t)

	// The signature is checked, not the shape. This flips one character of the
	// payload, which leaves a token that parses, carries the right issuer and
	// the right audience, and does not verify.
	c := w.login(t, fixtures.WorkerAID)
	parts := strings.Split(c.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT has three parts, this has %d", len(parts))
	}
	code, body := w.refusedToken(t, parts[0]+"."+flipLast(parts[1])+"."+parts[2])
	if code != http.StatusUnauthorized {
		t.Fatalf("a tampered token was answered %d, not 401: %s", code, body)
	}
}

// flipLast changes the last base64url character of a segment, so the payload
// decodes to something the signature does not cover.
func flipLast(seg string) string {
	if seg == "" {
		return seg
	}
	replacement := byte('A')
	if seg[len(seg)-1] == 'A' {
		replacement = 'B'
	}
	return seg[:len(seg)-1] + string(replacement)
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	w := setup(t)

	// A token that was valid is not a token that is valid. Minted already
	// expired rather than waiting one out, because the identity provider's
	// clock is real time and the harness cannot move it.
	token, err := w.oidc.TokenWith(w.ctx, map[string]any{
		"sub": runID + "|expired", "aud": w.oidc.Audience, "expiresIn": "-1h",
	})
	if err != nil {
		t.Fatalf("mint an expired token: %v", err)
	}
	code, body := w.refusedToken(t, token)
	if code != http.StatusUnauthorized {
		t.Fatalf("an expired token was answered %d, not 401: %s", code, body)
	}
}

func TestATokenForAnotherAudienceIsRefused(t *testing.T) {
	w := setup(t)

	// Correctly signed by the right issuer and still not ours. This is what
	// stops a token handed to one system being replayed into another.
	token, err := w.oidc.TokenWith(w.ctx, map[string]any{
		"sub": runID + "|elsewhere", "aud": "some-other-system", "expiresIn": "1h",
	})
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	code, body := w.refusedToken(t, token)
	if code != http.StatusUnauthorized {
		t.Fatalf("a token for another audience was answered %d, not 401: %s", code, body)
	}
}

func TestAnAuthenticatedStrangerIsNotAnybodyHere(t *testing.T) {
	w := setup(t)

	// Authenticated by the provider and unknown to this deployment. This is an
	// ordinary state — somebody the national system knows who has not been
	// enrolled here — and it has to read as "enrol me", not as "you are
	// rejected". 403 with subject_not_enrolled, never 401.
	token, err := w.oidc.Token(w.ctx, runID+"|stranger")
	if err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	worker := w.newWorker(t, "Stranger's Target "+runID)
	consentID := w.consentOf(t, worker)

	code, body, err := w.Parties.As(harness.Caller{Token: token}).
		Status(w.ctx, http.MethodPost, "/v1/consents/"+consentID+"/withdraw",
			map[string]any{"reason": "no"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("an unenrolled subject was answered %d, not 403: %s", code, body)
	}
	if !strings.Contains(string(body), "subject_not_enrolled") {
		t.Fatalf("the refusal does not say the subject is unenrolled: %s", body)
	}
}

func TestOneSubjectCannotBeTwoPeople(t *testing.T) {
	w := setup(t)

	// The rule 0008's unique index holds. A subject already bound to one party
	// reaching a second is not a re-binding — it is a login that would
	// authenticate as two people, and the service would then pick one.
	first := w.newWorker(t, "Subject Owner "+runID)
	second := w.newWorker(t, "Subject Claimant "+runID)
	binding := map[string]any{
		"provider":      "mock-oidc",
		"providerClass": "generic-oidc",
		"subjectRef":    w.oidc.Subject(runID + "|shared-subject"),
	}
	if err := w.Parties.As(w.assist(t, fixtures.SupervisorID, first)).Post(w.ctx,
		"/v1/parties/"+first+"/identity-bindings?contextId="+url.QueryEscape(fixtures.ProjectID), binding, nil); err != nil {
		t.Fatalf("bind the first party: %v", err)
	}
	code, body, err := w.Parties.As(w.assist(t, fixtures.SupervisorID, second)).Status(w.ctx, http.MethodPost,
		"/v1/parties/"+second+"/identity-bindings?contextId="+url.QueryEscape(fixtures.ProjectID), binding)
	if err != nil {
		t.Fatalf("bind the second party: %v", err)
	}
	if code != http.StatusConflict {
		t.Fatalf("binding one subject to a second party was answered %d, not 409: %s", code, body)
	}
}

func TestAnOrganisationApprovalNamesSomebodyWhoProvedIt(t *testing.T) {
	w := setup(t)

	// The self-approval constraint was always a check on a value the caller
	// chose. An applicant could satisfy it by naming anybody at all, which
	// makes "an approval you can grant yourself is not an approval" a rule
	// about typing rather than about people. Naming the specifier while being
	// the organisation is now refused.
	orgID := w.apply(t, "Impersonating Trust "+runID)

	code, body, err := w.Parties.As(w.login(t, orgID)).Status(w.ctx, http.MethodPost,
		"/v1/organisations/"+orgID+"/decision",
		map[string]any{"approve": true, "decidedBy": fixtures.SpecifierID})
	if err != nil {
		t.Fatalf("decision: %v", err)
	}
	if code != http.StatusForbidden {
		t.Fatalf("approving while naming somebody else was answered %d, not 403: %s", code, body)
	}
	if !strings.Contains(string(body), "party_not_proven") {
		t.Fatalf("the refusal does not name the impersonation: %s", body)
	}
}

// confirmClaim confirms as the worker whose claim it is.
//
// The party comes from the window rather than from the caller, which is the
// same direction the service checks it in: the record knows whose claim it is,
// and the request has to match. Scenarios about the work call this instead of
// posting directly, so that adding a confirmation to a scenario cannot
// accidentally add an unauthenticated one.
func (w *world) confirmClaim(t *testing.T, claimID string, body, out any) error {
	t.Helper()
	win, err := w.window(claimID)
	if err != nil {
		t.Fatalf("read the window for %s: %v", claimID, err)
	}
	return w.Confirmation.As(w.login(t, win.PartyID)).
		Post(w.ctx, "/v1/claims/"+claimID+"/confirm", body, out)
}

// disputeClaim raises a dispute as whoever the dispute says raised it.
//
// Note the difference from confirmClaim, which is not an oversight: a
// confirmation is somebody acting as the worker, and a dispute is somebody
// acting as themselves about the worker's record. A supervisor contesting a
// claim is legitimate and is not assistance.
func (w *world) disputeClaim(t *testing.T, claimID string, body map[string]any, out any) error {
	t.Helper()
	raisedBy, _ := body["raisedByPartyId"].(string)
	if raisedBy == "" {
		t.Fatalf("a dispute needs a raisedByPartyId to authenticate as")
	}
	return w.Confirmation.As(w.login(t, raisedBy)).
		Post(w.ctx, "/v1/claims/"+claimID+"/dispute", body, out)
}

// withdraw takes a party's consent back, as that party.
func (w *world) withdraw(t *testing.T, consentID, party string, body, out any) error {
	t.Helper()
	return w.Parties.As(w.login(t, party)).
		Post(w.ctx, "/v1/consents/"+consentID+"/withdraw", body, out)
}

func TestASupervisorConfirmsForAWorkerAndItIsRecordedAsAssisted(t *testing.T) {
	w := setup(t)

	// The supervisor-assisted T=7 exit, end to end. It is one of the four
	// routes out of a confirmation window and the only one available to a
	// worker who cannot operate a phone — so a tightening of authentication
	// that closed it would take the exit away from exactly the people it
	// exists for.
	phone := sharedNumber(201)
	worker := newWorkerWithPhone(t, w, "Assisted Confirmer", phone)
	res := w.submit(t, batch(row(phone, 7, "HH-assisted-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %+v", res)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.As(w.assist(t, fixtures.SupervisorID, worker)).
		Post(w.ctx, "/v1/claims/"+claimID+"/confirm", nil, &exit); err != nil {
		t.Fatalf("a supervisor could not confirm for a worker: %v", err)
	}
	if exit.Credential.ID == "" {
		t.Fatal("an assisted confirmation produced no credential")
	}

	// And it says so. An assisted confirmation stored as the worker's own is
	// indistinguishable afterwards from one they never made, which is the
	// whole reason the route is on the record.
	win, err := w.window(claimID)
	if err != nil {
		t.Fatal(err)
	}
	if win.ExitRoute == nil || *win.ExitRoute != "assisted" {
		t.Fatalf("the exit route is %v, not assisted", win.ExitRoute)
	}
}

func TestAConfirmationCannotClaimToBeSomethingItIsNot(t *testing.T) {
	w := setup(t)

	// The route is what happened, not what was asked for. A caller confirming
	// as themselves cannot record it as assisted, and the reverse is refused
	// by the same check — because a route nobody can rely on is a route that
	// says nothing about whether the worker was ever asked.
	phone := sharedNumber(202)
	newWorkerWithPhone(t, w, "Honest Route", phone)
	res := w.submit(t, batch(row(phone, 5, "HH-route-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %+v", res)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})

	win, err := w.window(claimID)
	if err != nil {
		t.Fatal(err)
	}
	code, body, err := w.Confirmation.As(w.login(t, win.PartyID)).Status(w.ctx, http.MethodPost,
		"/v1/claims/"+claimID+"/confirm", map[string]any{"route": "assisted"})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if code != http.StatusBadRequest {
		t.Fatalf("claiming an assisted route while acting alone was answered %d, not 400: %s", code, body)
	}
	if !strings.Contains(string(body), "route_contradicts_caller") {
		t.Fatalf("the refusal does not name the contradiction: %s", body)
	}

	// The window is still open — a refused confirmation must not have exited
	// it, or the worker has lost their chance to confirm at all.
	if again, err := w.window(claimID); err != nil || again.ExitRoute != nil {
		t.Fatalf("the window exited despite the refusal: %+v, %v", again.ExitRoute, err)
	}
}

func TestAnonymousCallersCannotMintOrListAuthorizations(t *testing.T) {
	w := setup(t)

	// A grant decides who may act for whom; minting one anonymously would let
	// anyone authorise anyone (#102). The gate must answer before the body is
	// even read, so an empty object is enough to prove the door is shut.
	code, body, err := w.Parties.Status(w.ctx, http.MethodPost,
		"/v1/authorizations", map[string]any{})
	if err != nil {
		t.Fatalf("post an authorization: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("an anonymous authorization mint was answered %d, not 401: %s", code, body)
	}

	// And the list read: who holds what, where — the roster probe #68
	// established must not be readable — is signed-in only.
	code, body, err = w.Parties.Status(w.ctx, http.MethodGet,
		"/v1/authorizations?partyId=anyone", nil)
	if err != nil {
		t.Fatalf("list authorizations: %v", err)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("an anonymous authorization list was answered %d, not 401: %s", code, body)
	}
}
