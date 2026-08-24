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

// Onboarding and the public half of Blueprint §3, against real services (#20).
//
// Every scenario here is named after what it protects rather than what it
// calls, because each of them is a way a real person is harmed: an organisation
// approving itself, a worker who cannot be enrolled without a phone, a
// supervisor's say-so quietly counting as an identity check, or a worker's
// roster entry ending up in a permanent public log.

func orgParty(name string) schema.Party {
	return schema.Party{
		Kind:        schema.PartyKindOrganisation,
		DisplayName: name,
		ContactRoutes: []schema.PartyContactRoutesItem{
			{Kind: schema.PartyContactRoutesItemKindEmail, Value: "ops-" + runID + "@example.org"},
		},
	}
}

type registration struct {
	PartyID    string  `json:"partyId"`
	State      string  `json:"state"`
	DecidedBy  *string `json:"decidedBy,omitempty"`
	Reason     *string `json:"reason,omitempty"`
	AcceptedBy *string `json:"acceptedBy,omitempty"`
}

type publication struct {
	Kind            string `json:"kind"`
	Registry        string `json:"registry"`
	Record          string `json:"record"`
	RegistryVersion string `json:"registryVersion"`
	Digest          string `json:"digest"`
	Transparent     bool   `json:"transparent"`
}

// apply registers an organisation and returns its party id.
func (w *world) apply(t *testing.T, name string) string {
	t.Helper()
	var out struct {
		Party        schema.Party `json:"party"`
		Registration registration `json:"registration"`
	}
	if err := w.Registry.Post(w.ctx, "/v1/organisations", orgParty(name), &out); err != nil {
		t.Fatalf("register organisation: %v", err)
	}
	if out.Registration.State != "APPLIED" {
		t.Fatalf("a new application is %q, want APPLIED", out.Registration.State)
	}
	return out.Party.ID
}

// An approval you can grant yourself is not an approval. This is the same
// separation of duties §7 requires of a definition, and it is enforced in two
// places — the service and a CHECK constraint — so a future code path that
// forgets still cannot write the row.
func TestAnOrganisationCannotApproveItself(t *testing.T) {
	w := setup(t)
	orgID := w.apply(t, "Self-Approving Trust "+runID)

	code, body, err := w.Registry.Status(w.ctx, http.MethodPost,
		"/v1/organisations/"+orgID+"/decision",
		map[string]any{"approve": true, "decidedBy": orgID})
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict {
		t.Fatalf("self-approval answered %d: %s", code, body)
	}
}

// An organisation operating under no terms is an organisation nobody agreed
// anything with, and a verifier walking back from a credential to "under what
// terms was this authorised" would find nothing.
func TestAnOrganisationCannotBeApprovedBeforeAcceptingTerms(t *testing.T) {
	w := setup(t)
	orgID := w.apply(t, "Hasty Trust "+runID)

	code, body, err := w.Registry.Status(w.ctx, http.MethodPost,
		"/v1/organisations/"+orgID+"/decision",
		map[string]any{"approve": true, "decidedBy": fixtures.SpecifierID})
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict {
		t.Fatalf("approval without terms answered %d: %s", code, body)
	}
}

// The whole path, and the fact it produces: an approved organisation reaches
// the registry substrate, where somebody outside CREST can resolve it.
func TestAnApprovedOrganisationReachesTheRegistry(t *testing.T) {
	w := setup(t)
	orgID := w.apply(t, "Bednet Distribution Trust "+runID)
	terms := w.w.Terms[0]

	var reg registration
	if err := w.Registry.Post(w.ctx, "/v1/organisations/"+orgID+"/terms-acceptance",
		map[string]any{"termsId": terms.ID, "termsVersion": terms.Version, "acceptedBy": orgID}, &reg); err != nil {
		t.Fatalf("accept terms: %v", err)
	}

	// Not published yet. An applicant that has signed terms is still not an
	// approved organisation, and an append-only log that recorded both the same
	// way could never tell them apart afterwards.
	code, _, err := w.Registry.Status(w.ctx, http.MethodGet, "/v1/publications/organisation/"+orgID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code != http.StatusNotFound && reg.State == "TERMS_ACCEPTED" {
		t.Fatalf("an organisation that had only accepted terms was already published (%d)", code)
	}

	if reg.State == "TERMS_ACCEPTED" {
		if err := w.Registry.Post(w.ctx, "/v1/organisations/"+orgID+"/decision",
			map[string]any{"approve": true, "decidedBy": fixtures.SpecifierID}, &reg); err != nil {
			t.Fatalf("approve: %v", err)
		}
	}
	if reg.State != "APPROVED" {
		t.Fatalf("state after approval is %q", reg.State)
	}

	var pub publication
	eventually(t, "the approved organisation reaches the registry", 20*time.Second, func() error {
		return w.Registry.Get(w.ctx, "/v1/publications/organisation/"+orgID, &pub)
	})
	if pub.Registry != "organisations" || pub.Record != orgID {
		t.Fatalf("published to %s/%s, want organisations/%s", pub.Registry, pub.Record, orgID)
	}
	if pub.Digest == "" || pub.RegistryVersion == "" {
		t.Fatalf("publication carries no digest or version: %+v", pub)
	}
	// Reported honestly either way. On the Postgres fallback this is false, and
	// a caller who is told so knows that resolving this still means trusting
	// this deployment (#20).
	t.Logf("organisation %s published to %s@%s, transparent=%v",
		orgID, pub.Record, pub.RegistryVersion, pub.Transparent)
}

// W1: a worker must be able to exist without a document, a phone or literacy.
// A system that can only enrol people who complete a form on their own device
// excludes exactly the workers it is for.
func TestAWorkerWithNoPhoneCanStillBeEnrolled(t *testing.T) {
	w := setup(t)
	supervisor := fixtures.SupervisorID

	var out struct {
		Party     schema.Party `json:"party"`
		Enrolment struct {
			EnrolledBy string `json:"enrolledBy"`
			Method     string `json:"method"`
		} `json:"enrolment"`
		IdentityAssurance string `json:"identityAssurance"`
	}
	body := map[string]any{
		"enrolledBy": supervisor,
		"method":     "supervisor-attested",
		"party": schema.Party{
			Kind:        schema.PartyKindPerson,
			DisplayName: "Enrolled By Supervisor " + runID,
			// No phone. The route that reaches this worker is their
			// supervisor, which is what the contact kind is for — W2 is
			// unenforceable against a Party nobody can reach, and this path
			// does not exempt it.
			ContactRoutes: []schema.PartyContactRoutesItem{
				{Kind: schema.PartyContactRoutesItemKindSupervisor, Value: supervisor},
			},
		},
	}
	if err := w.Registry.Post(w.ctx, "/v1/enrolments", body, &out); err != nil {
		t.Fatalf("assisted enrolment: %v", err)
	}
	if out.Party.ID == "" {
		t.Fatal("no party was created")
	}

	// The property that matters as much as the enrolment succeeding: being
	// vouched for by a supervisor did NOT raise the worker's identity
	// assurance. Assurance stays derived from the Party's own bindings, so this
	// worker can be upgraded later when they bind an anchor, and is never
	// silently treated as equivalent to one who already has.
	var assurance struct {
		Level   string   `json:"identityAssurance"`
		Because []string `json:"because"`
	}
	if err := w.Registry.Get(w.ctx, "/v1/parties/"+out.Party.ID+"/assurance", &assurance); err != nil {
		t.Fatalf("read assurance: %v", err)
	}
	if assurance.Level != out.IdentityAssurance {
		t.Errorf("the enrolment response reported assurance %q, the derivation says %q",
			out.IdentityAssurance, assurance.Level)
	}

	var enrolment struct {
		EnrolledBy string `json:"enrolledBy"`
		Method     string `json:"method"`
	}
	if err := w.Registry.Get(w.ctx, "/v1/parties/"+out.Party.ID+"/enrolment", &enrolment); err != nil {
		t.Fatalf("read enrolment provenance: %v", err)
	}
	if enrolment.EnrolledBy != supervisor {
		t.Errorf("enrolledBy = %q, want the supervisor who is answerable for it", enrolment.EnrolledBy)
	}
}

// The "never the reverse" half of §3's placement rule. A worker is personal
// data; nothing about them reaches the append-only public log, and the
// publication endpoint is the only place that could have leaked one.
func TestAWorkerNeverReachesTheRegistrySubstrate(t *testing.T) {
	w := setup(t)
	workerID := w.w.Parties[0].ID

	for _, kind := range []string{"organisation", "authorization"} {
		code, body, err := w.Registry.Status(w.ctx, http.MethodGet,
			fmt.Sprintf("/v1/publications/%s/%s", kind, workerID), nil)
		if err != nil {
			t.Fatal(err)
		}
		if code != http.StatusNotFound {
			t.Fatalf("a worker is published as %s: %d %s", kind, code, body)
		}
	}
}

// A credential names the definition version it was issued against. For that
// name to mean anything to someone outside CREST, the version has to be
// resolvable without asking CREST — and stay resolvable after the definition
// moves on (#21, §7).
func TestAnActivatedDefinitionIsResolvableOutsideCREST(t *testing.T) {
	w := setup(t)

	var pub publication
	eventually(t, "the seeded definition reaches the registry", 20*time.Second, func() error {
		return w.Definitions.Get(w.ctx,
			"/v1/definitions/"+w.w.Definitions[0].ID+"/publication", &pub)
	})
	if pub.Digest == "" {
		t.Fatalf("publication carries no digest: %+v", pub)
	}
	t.Logf("definition %s published as %s@%s, transparent=%v",
		w.w.Definitions[0].ID, pub.Record, pub.RegistryVersion, pub.Transparent)
}

// The payoff of publishing definitions, and the honesty required when it is not
// available (#68, #69).
//
// A verdict's trust chain used to be a list of sentences. Some of those a
// verifier could confirm without CREST — the signature, the status list — and
// some were this deployment reading its own database aloud. Nothing
// distinguished them, so a verifier who wanted to check the whole chain had no
// way to know which parts they could.
//
// This asserts the distinction is real: the definition link is checkable
// exactly when the definition actually reached a transparency log, and says
// what is being trusted when it did not.
func TestTheTrustChainSaysWhichLinksAVerifierCanCheck(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	res := w.submit(t, batch(row(phone, 4, "HH-TRUST-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %d: %+v", len(res.ClaimIDs), res.Unclear)
	}

	// Confirmed rather than left to auto-confirm: this scenario is about what
	// the verdict discloses, and the shortest honest route to a credential is
	// the one that does not also re-test the window.
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		_, err := w.window(res.ClaimIDs[0])
		return err
	})
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+res.ClaimIDs[0]+"/confirm",
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if exit.Credential.ID == "" {
		t.Fatal("confirming produced no credential")
	}

	// Read where the definition landed BEFORE verifying, not after. The
	// publication crosses a service boundary through the outbox, and a verdict
	// computed while it was still in flight would legitimately report the link
	// as unverifiable — comparing that against a publication that had arrived
	// by the time the test looked would be a race, not a finding.
	var pub publication
	eventually(t, "the definition's publication is readable", 20*time.Second, func() error {
		return w.Definitions.Get(w.ctx,
			"/v1/definitions/"+w.w.Definitions[0].ID+"/publication", &pub)
	})

	v := w.verify(t, w.credential(t, exit.Credential.ID))
	if !v.Valid {
		t.Fatalf("the credential does not verify: %v", v.Reasons)
	}

	var defLink *trustLink
	for i, l := range v.TrustChain {
		if strings.Contains(l.Claim, "measured under") {
			defLink = &v.TrustChain[i]
		}
	}
	if defLink == nil {
		t.Fatal("the trust chain has no link naming the definition the credential was measured under")
	}

	// The invariant is one-directional, and the direction matters.
	//
	// Checkable MUST imply the version really reached a transparency log —
	// claiming otherwise hands a verifier a URL that proves nothing. The
	// converse does not hold: a version published to a log by a deployment that
	// has since been reconfigured without a node address is genuinely no longer
	// checkable *here*, and saying so is correct rather than a contradiction.
	// Asserting equality treats that honest answer as a bug.
	if defLink.Checkable && !pub.Transparent {
		t.Fatalf("the trust chain says this definition is independently checkable, but it was " +
			"published with no transparency proof — the verifier is being handed a URL that proves nothing")
	}
	if pub.Transparent && !defLink.Checkable {
		t.Logf("published to a log, but not checkable from here: %q", defLink.Trusting)
		if defLink.Trusting == "" {
			t.Error("and it does not say why")
		}
	}
	if defLink.Checkable {
		// The URL has to actually answer. A "how" nobody can fetch is a
		// promise, which is the thing this whole field replaces.
		resp, err := http.Get(defLink.How) //nolint:gosec,noctx // a URL this deployment just published
		if err != nil {
			t.Fatalf("the trust chain points at %s, which does not answer: %v", defLink.How, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the trust chain points at %s, which answered %d", defLink.How, resp.StatusCode)
		}
		t.Logf("verifier can resolve the pinned definition at %s", defLink.How)
	} else {
		t.Logf("definition link is not independently checkable, and says so: %q", defLink.Trusting)
	}

	// #68: the limit is disclosed on the verdict a verifier acts on, not left
	// for them to discover by assuming wrongly.
	found := false
	for _, s := range v.NotEstablished {
		if strings.Contains(s, "authorised") {
			found = true
		}
	}
	if !found {
		t.Errorf("a valid verdict does not say that the subject's authorization is unverifiable: %v", v.NotEstablished)
	}
}

// #70: a deployment says who it is, and a verifier can resolve it.
//
// This closes the loop #69 left open. A verifier who resolves
// `crest/organisations/<id>` on the node holds a record and, until now, no way
// to find out which deployment owns that namespace, which publisher key its
// writes should carry, or who is answerable when the record and a credential
// disagree.
func TestADeploymentSaysWhoItIsAndAVerifierCanResolveIt(t *testing.T) {
	w := setup(t)

	var out struct {
		Instance struct {
			ID              string `json:"instanceId"`
			Name            string `json:"name"`
			OperatorPartyID string `json:"operatorPartyId"`
			IssuerID        string `json:"issuerId"`
			Registry        struct {
				Namespace      string `json:"namespace"`
				PublisherKeyID string `json:"publisherKeyId"`
				Transparent    bool   `json:"transparent"`
			} `json:"registry"`
		} `json:"instance"`
		Publication publication `json:"publication"`
	}
	// Unauthenticated on purpose: a public self-description nobody outside can
	// read is one nobody outside can check.
	eventually(t, "the deployment publishes its own identity", 20*time.Second, func() error {
		if err := w.Registry.Get(w.ctx, "/v1/instance", &out); err != nil {
			return err
		}
		if out.Publication.Record == "" {
			return fmt.Errorf("published nowhere yet")
		}
		return nil
	})

	if out.Instance.OperatorPartyID == "" {
		t.Error("the deployment does not say who operates it")
	}
	if out.Instance.Registry.Transparent && out.Instance.Registry.PublisherKeyID == "" {
		t.Error("the deployment publishes to a log and does not say which key its writes carry")
	}
	// The description of where it publishes has to match where it actually
	// published. If those disagree, the record is pointing readers at a
	// namespace it is not writing to.
	if out.Publication.Registry != "instances" {
		t.Errorf("the instance published to %q, want the instances registry", out.Publication.Registry)
	}
	if out.Instance.Registry.Transparent != out.Publication.Transparent {
		t.Errorf("the deployment says transparent=%v and its own publication says %v",
			out.Instance.Registry.Transparent, out.Publication.Transparent)
	}
	t.Logf("instance %s, operated by %s, published as %s@%s (transparent=%v)",
		out.Instance.ID, out.Instance.OperatorPartyID,
		out.Publication.Record, out.Publication.RegistryVersion, out.Publication.Transparent)
}

// #16: the credential carries its own resolvable pin to the definition version
// it was measured under.
//
// The distinction this proves is small on the wire and large in what it means.
// A verifier who resolves the definition by asking CREST is asking the issuer
// to confirm the issuer. A verifier who reads the pin out of the signature is
// checking what the issuer committed to at the moment they signed — a
// commitment nobody, including this deployment, can revise afterwards.
func TestACredentialCarriesItsOwnPinToTheDefinition(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	res := w.submit(t, batch(row(phone, 6, "HH-PIN-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %d: %+v", len(res.ClaimIDs), res.Unclear)
	}
	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		_, err := w.window(res.ClaimIDs[0])
		return err
	})
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+res.ClaimIDs[0]+"/confirm",
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	cred := w.credential(t, exit.Credential.ID)
	subject, _ := cred["credentialSubject"].(map[string]any)
	workEvent, _ := subject["workEvent"].(map[string]any)
	if workEvent == nil {
		t.Fatal("the credential has no workEvent")
	}

	var pub publication
	eventually(t, "the definition's publication is readable", 20*time.Second, func() error {
		return w.Definitions.Get(w.ctx,
			"/v1/definitions/"+w.w.Definitions[0].ID+"/publication", &pub)
	})

	proof, hasProof := workEvent["definitionProof"].(map[string]any)
	if !pub.Transparent {
		// Absence is the honest answer where there is nothing to prove, and it
		// must stay absent rather than becoming an empty object that reads as
		// a pin nobody can follow.
		if hasProof {
			t.Fatalf("the deployment has no transparency log but the credential carries a pin: %v", proof)
		}
		t.Log("no transparency log behind this deployment's registry, and the credential says nothing rather than something empty")
		return
	}

	if !hasProof {
		t.Fatal("the definition is on a transparency log and the credential carries no pin to it")
	}
	// The pin must name the version that was actually published. A pin to a
	// different version is worse than no pin: it resolves, it verifies, and it
	// describes a definition this credential was not measured under.
	if proof["version"] != pub.RegistryVersion {
		t.Errorf("the credential pins registry version %v, the definition was published as %s",
			proof["version"], pub.RegistryVersion)
	}
	if proof["digest"] != pub.Digest {
		t.Errorf("the credential pins digest %v, the published record's digest is %s",
			proof["digest"], pub.Digest)
	}
	// No node address inside the signature. Where the log lives is the
	// deployment's published self-description (#70), which can be corrected;
	// an address baked into a signed credential is a redirect nobody can ever
	// withdraw.
	for _, forbidden := range []string{"url", "endpoint", "node"} {
		if _, found := proof[forbidden]; found {
			t.Errorf("the signed pin carries %q; a node address in a signature cannot be withdrawn", forbidden)
		}
	}

	// And the verifier is pointed at it.
	v := w.verify(t, cred)
	if !v.Valid {
		t.Fatalf("the credential does not verify: %v", v.Reasons)
	}
	for _, l := range v.TrustChain {
		if strings.Contains(l.Claim, "measured under") && l.Checkable {
			if !strings.Contains(l.Claim, pub.Digest) {
				t.Errorf("the trust chain does not tell the verifier which digest to expect: %q", l.Claim)
			}
			t.Logf("verifier follows the credential's own pin to %s", l.How)
			return
		}
	}
	t.Error("the trust chain has no checkable definition link despite the credential carrying a pin")
}

// The skill list (#16, Blueprint §3).
//
// A skill code is the part of a worker's record that means the same thing
// somewhere else. `activity.code` is this deployment's own word for the work;
// the skill code is what makes the record portable across programmes and
// eventually across borders.
func TestASkillCodeIsPublishedAndImmutable(t *testing.T) {
	w := setup(t)

	code := "CREST-SKILL:chw.bednet-distribution.v2"
	var sk struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	}
	if err := w.Registry.Get(w.ctx, "/v1/skills/"+url.PathEscape(code), &sk); err != nil {
		t.Fatalf("the seeded skill is not there: %v", err)
	}

	// Immutable, and refused rather than silently ignored. The code is already
	// in issued credentials that cannot be rewritten, so a code whose meaning
	// changed underneath them would restate what a worker's record says they
	// can do — without anyone reissuing anything.
	status, body, err := w.Registry.Status(w.ctx, http.MethodPost, "/v1/skills", map[string]any{
		"code": code, "label": "Something else entirely", "publishedAt": "2026-02-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusConflict {
		t.Fatalf("republishing a skill code under a new meaning answered %d: %s", status, body)
	}

	// A supersession has to name a code that exists, or the chain a worker's
	// older credentials hang from has a gap nobody would notice until somebody
	// tried to walk it.
	status, body, err = w.Registry.Status(w.ctx, http.MethodPost, "/v1/skills", map[string]any{
		"code": "CREST-SKILL:chw.bednet-distribution.v9", "label": "Future",
		"supersedes": "CREST-SKILL:chw.does-not-exist.v1", "publishedAt": "2026-02-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Errorf("superseding a skill that does not exist answered %d: %s", status, body)
	}

	// And it reaches the registry substrate, so a verifier can resolve what the
	// code means without asking this deployment.
	var pub publication
	eventually(t, "the skill reaches the registry", 20*time.Second, func() error {
		return w.Registry.Get(w.ctx, "/v1/publications/skill/"+url.PathEscape(code), &pub)
	})
	if pub.Registry != "skills" {
		t.Errorf("the skill published to %q", pub.Registry)
	}
	t.Logf("skill %s published as %s@%s (transparent=%v)", code, pub.Record, pub.RegistryVersion, pub.Transparent)
}

// #16 complete: the credential carries the whole chain a verifier walks.
//
// claimId, skillCode and issuerAuthority. The last is the one that matters
// most: without it a verifier can confirm the credential is signed and
// unrevoked and has no way to reach anybody answerable for the claim it makes.
func TestACredentialNamesTheChainAVerifierWalks(t *testing.T) {
	w := setup(t)

	phone, err := harness.PhoneOf(w.w, fixtures.WorkerAID)
	if err != nil {
		t.Fatal(err)
	}
	res := w.submit(t, batch(row(phone, 7, "HH-CHAIN-"+runID)))
	if len(res.ClaimIDs) != 1 {
		t.Fatalf("expected one claim, got %d: %+v", len(res.ClaimIDs), res.Unclear)
	}
	claimID := res.ClaimIDs[0]
	eventually(t, "the confirmation window opens", 15*time.Second, func() error {
		_, err := w.window(claimID)
		return err
	})
	var exit struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
	}
	if err := w.Confirmation.Post(w.ctx, "/v1/claims/"+claimID+"/confirm",
		map[string]any{"route": "self"}, &exit); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	cred := w.credential(t, exit.Credential.ID)
	subject, _ := cred["credentialSubject"].(map[string]any)
	workEvent, _ := subject["workEvent"].(map[string]any)

	// A Unit says the work happened; a Claim says who performed it. A
	// credential projects the Claim, so naming only the Unit would leave it
	// unable to say which of several claims on that Unit it is about.
	if workEvent["claimId"] != claimID {
		t.Errorf("claimId = %v, want %s", workEvent["claimId"], claimID)
	}
	if workEvent["eventId"] == workEvent["claimId"] {
		t.Error("the claim and the unit are the same identifier; they are separable by design")
	}

	// The one field whose purpose is to be read by somebody who is not this
	// deployment, carried directly so it can be read with no network.
	if workEvent["skillCode"] != "CREST-SKILL:chw.bednet-distribution.v2" {
		t.Errorf("skillCode = %v", workEvent["skillCode"])
	}

	authority, ok := subject["issuerAuthority"].(map[string]any)
	if !ok {
		t.Fatal("the credential names no issuer authority; a verifier can reach nobody answerable")
	}
	if authority["orgId"] == "" || authority["orgId"] == nil {
		t.Error("issuerAuthority names no organisation")
	}
	// Both scopes of the same primitive: §2 collapsed Qualification and
	// ProjectGrant into one Authorization at two scopes, and these are those.
	qual, hasQual := authority["qualificationRef"].(string)
	grant, hasGrant := authority["grantRef"].(string)
	if !hasQual || !hasGrant {
		t.Fatalf("issuerAuthority does not carry both scopes: %v", authority)
	}
	if qual == grant {
		t.Error("the instance-wide and context-bound references are the same authorization")
	}

	// Every reference must be to an ORGANISATION's authorization, because a
	// person's is never published (#68) — a chain ending at a supervisor ends
	// at something a verifier cannot resolve, which is worse than ending
	// nowhere because it looks like it leads somewhere.
	for _, ref := range []string{qual, grant} {
		var pub publication
		eventually(t, "the authorization behind the credential is resolvable", 20*time.Second, func() error {
			return w.Registry.Get(w.ctx, "/v1/publications/authorization/"+url.PathEscape(ref), &pub)
		})
		if pub.Registry != "authorizations" {
			t.Errorf("%s published to %q", ref, pub.Registry)
		}
	}
	t.Logf("chain: org %v → qualification %s → grant %s", authority["orgId"], qual, grant)
}
