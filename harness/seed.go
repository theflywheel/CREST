package harness

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/pii"
	"github.com/theflywheel/crest/pkg/schema"
)

// Seed loads the canonical fixture world into a running stack, through the
// same HTTP endpoints a real deployment is set up through.
//
// Through the real endpoints on purpose. A seeder that writes rows directly
// would let the world contain things the services would have refused — a
// self-ratified definition, an authorization with no approver — and every test
// built on it would then agree with a bug.
func (s *Stack) Seed(ctx context.Context) (*fixtures.World, error) {
	return s.SeedAt(ctx, time.Time{})
}

// SeedAt is Seed with the fixture world slid onto a different epoch.
//
// A zero epoch keeps the file's own dates, which is what the scenarios want: a
// test asserting on a date should not depend on the day it runs. A demo
// deployment passes a real one, so the programme's dates sit around today
// rather than around whenever the fixture file was last edited.
func (s *Stack) SeedAt(ctx context.Context, epoch time.Time) (*fixtures.World, error) {
	w, err := fixtures.LoadAt(epoch)
	if err != nil {
		return nil, err
	}

	if err := s.SetClock(ctx, w.Instance.Epoch); err != nil {
		return nil, fmt.Errorf("set clock: %w", err)
	}

	for _, p := range w.Parties {
		if err := s.Parties.Post(ctx, "/v1/parties", p, nil); err != nil {
			return nil, fmt.Errorf("party %s: %w", p.DisplayName, err)
		}
	}
	// Skills before definitions: a definition names a skill code, and a
	// deployment that has not adopted the taxonomy would be publishing a
	// definition pointing at a vocabulary entry nobody can resolve.
	for _, sk := range w.Skills {
		// A skill code already published is the normal case on a stack that is
		// already up, and refusing it is right — the code is immutable — so
		// the seeder skips rather than treating "it is already correct" as a
		// failure.
		if err := s.Parties.Get(ctx, "/v1/skills/"+url.PathEscape(sk.Code), nil); err == nil {
			continue
		}
		if err := s.Parties.Post(ctx, "/v1/skills", sk, nil); err != nil {
			return nil, fmt.Errorf("skill %s: %w", sk.Code, err)
		}
	}
	for _, t := range w.Terms {
		if err := s.Parties.Post(ctx, "/v1/terms", t, nil); err != nil {
			return nil, fmt.Errorf("terms %s: %w", t.Name, err)
		}
	}
	for _, c := range w.Contexts {
		if err := s.Parties.Post(ctx, "/v1/contexts", c, nil); err != nil {
			return nil, fmt.Errorf("context %s: %w", c.Name, err)
		}
	}
	// Minting a grant is the named authority saying so, and every fixture
	// grant names the organisation — so the seeder proves it IS the
	// organisation, exactly as a first login would: mint a token from the
	// stack's own mock issuer and self-bind its subject to the org party,
	// which the never-yet-bound org accepts as bootstrap. The rest of the
	// seed goes through doors that are deliberately open (parties, terms,
	// contexts).
	oidc := NewOIDC()
	if err := oidc.WaitReady(ctx, 60*time.Second); err != nil {
		return nil, fmt.Errorf("identity provider: %w", err)
	}
	// The organisation must be APPROVED before it can be an authority: the
	// grant gate reads the registration, not the party's shape. The seeder
	// walks the same application → terms → decision path a real organisation
	// takes, resuming from wherever a previous seed left the registration.
	//
	// This happens BEFORE the seeder binds itself to the organisation, because
	// applying re-writes the party from the fixture document, and the identity
	// keys are rebuilt from that document — a binding made first would be
	// silently wiped, and every later seeded grant would fail as
	// subject_not_enrolled.
	if err := s.approveOrganisation(ctx, oidc, w); err != nil {
		return nil, err
	}
	token, err := oidc.Token(ctx, "seed|custodian")
	if err != nil {
		return nil, fmt.Errorf("mint seeding token: %w", err)
	}
	asSeeder := s.Parties.As(Caller{Token: token})
	if err := asSeeder.Post(ctx, "/v1/parties/"+fixtures.OrgID+"/identity-bindings",
		map[string]any{
			"provider":      "mock-oidc",
			"providerClass": "generic-oidc",
			"subjectRef":    oidc.Subject("seed|custodian"),
		}, nil); err != nil {
		return nil, fmt.Errorf("bind the seeder to the organisation: %w", err)
	}
	for _, a := range w.Authorizations {
		if err := asSeeder.Post(ctx, "/v1/authorizations", a, nil); err != nil {
			return nil, fmt.Errorf("authorization %s: %w", a.ID, err)
		}
	}

	// The definition goes in as a DRAFT and is then ratified and activated, so
	// seeding exercises the same separation-of-duties path a real one takes
	// (§7). Seeding straight into ACTIVE would skip the only rule the
	// definitions service exists to enforce.
	//
	// Seeding is idempotent because scenarios run against one stack: a second
	// scenario finding the definition already ACTIVE is the normal case, not a
	// failure. What is *not* idempotent is creating the version twice — the
	// service refuses that, and it is right to, because a definition version is
	// immutable from the moment it exists.
	// Which issuer this deployment actually signs with.
	//
	// The fixture names one (`authorisedIssuers`), and verification refuses a
	// credential from an issuer the definition does not list — correctly, it is
	// the check doing its job. But the name in the file is only right for a
	// stack running the default ISSUER_ID. On the Railway demo, which signs as
	// did:crest:issuer:railway-demo, every credential the story issued failed
	// verification with "not an authorised issuer", and had done so for as long
	// as that deployment existed: the seeder published a definition that did
	// not authorise the only issuer in the deployment.
	//
	// So the deployment's issuer is asked for rather than assumed, and added to
	// whatever the fixture declared. Added, not replaced — the fixture's own
	// entry is what a local stack verifies against, and dropping it would move
	// the breakage rather than fix it.
	var issuer struct {
		Issuer string `json:"issuer"`
	}
	if err := s.Confirmation.Get(ctx, "/v1/issuer", &issuer); err != nil {
		return nil, fmt.Errorf("ask the confirmation service which issuer it signs with: %w", err)
	}
	if issuer.Issuer == "" {
		return nil, fmt.Errorf("the confirmation service named no issuer; a definition cannot authorise one")
	}

	for _, d := range w.Definitions {
		var existing schema.Definition
		if err := s.Definitions.Get(ctx, "/v1/definitions/"+d.ID, &existing); err == nil &&
			existing.State == schema.DefinitionStateACTIVE {
			continue
		}
		ratifier := d.RatifiedByPartyID
		draft := d
		draft.Faces.Verifier.AuthorisedIssuers = withIssuer(
			d.Faces.Verifier.AuthorisedIssuers, issuer.Issuer)
		draft.State = schema.DefinitionStateDRAFT
		draft.RatifiedByPartyID = nil
		draft.ActivatedAt = nil
		if err := s.Definitions.Post(ctx, "/v1/definitions", draft, nil); err != nil {
			return nil, fmt.Errorf("definition %s: %w", d.ID, err)
		}
		if ratifier == nil {
			return nil, fmt.Errorf("definition %s has no ratifier in the fixture world", d.ID)
		}
		path := fmt.Sprintf("/v1/definitions/%s/versions/%d", d.ID, d.Version)
		if err := s.Definitions.Post(ctx, path+"/ratify",
			map[string]any{"ratifiedByPartyId": *ratifier}, nil); err != nil {
			return nil, fmt.Errorf("ratify %s: %w", d.ID, err)
		}
		if err := s.Definitions.Post(ctx, path+"/activate", nil, nil); err != nil {
			return nil, fmt.Errorf("activate %s: %w", d.ID, err)
		}
	}

	for _, lr := range w.LinkedRecords {
		if lr.KeyedTo.Kind != schema.LinkedRecordKeyedToKindDefinition {
			continue
		}
		if err := s.Definitions.Post(ctx,
			"/v1/definitions/"+lr.KeyedTo.ID+"/linked-records", lr, nil); err != nil {
			return nil, fmt.Errorf("linked record %s: %w", lr.ID, err)
		}
	}
	return w, nil
}

// withIssuer returns the list with id in it, appending only if it is missing.
// A definition version is immutable once it exists, so this has to be right
// the first time rather than patched afterwards.
func withIssuer(list []string, id string) []string {
	for _, existing := range list {
		if existing == id {
			return list
		}
	}
	out := make([]string, len(list), len(list)+1)
	copy(out, list)
	return append(out, id)
}

// PhoneOf returns a party's phone number, which is what a CSV joins on.
func PhoneOf(w *fixtures.World, partyID string) (string, error) {
	p, ok := w.Party(partyID)
	if !ok {
		return "", fmt.Errorf("no party %s", partyID)
	}
	for _, r := range p.ContactRoutes {
		if r.Kind == schema.PartyContactRoutesItemKindPhone {
			return r.Value, nil
		}
	}
	return "", fmt.Errorf("%s has no phone route", partyID)
}

// LocalHasher is the salt the local stack runs with. The harness needs it to
// build the same national-identifier hash the pipeline will compute — never to
// reverse one, which nothing can.
func LocalHasher() (*pii.Hasher, error) {
	return pii.NewHasher(
		env("NATIONAL_ID_SALT", "local-development-salt-not-for-any-real-deployment"),
		env("NATIONAL_ID_SALT_REF", "local-1"))
}

// Epoch is the fixture world's starting instant, for tests that need to reason
// about dates without loading the world.
func Epoch() time.Time { return time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC) }

// approveOrganisation walks the fixture organisation through the registry's
// onboarding — application, terms, decision — because an authority is an
// APPROVED organisation, not an organisation-shaped party. It resumes from
// wherever an earlier seed left the registration, so reseeding stays
// idempotent.
func (s *Stack) approveOrganisation(ctx context.Context, oidc *OIDC, w *fixtures.World) error {
	var reg struct {
		State string `json:"state"`
	}
	if err := s.Parties.Get(ctx,
		"/v1/organisations/"+fixtures.OrgID+"/registration", &reg); err != nil {
		var org *schema.Party
		for i := range w.Parties {
			if w.Parties[i].ID == fixtures.OrgID {
				org = &w.Parties[i]
			}
		}
		if org == nil {
			return fmt.Errorf("the fixture organisation %s is not among the fixture parties", fixtures.OrgID)
		}
		var out struct {
			Registration struct {
				State string `json:"state"`
			} `json:"registration"`
		}
		if err := s.Parties.Post(ctx, "/v1/organisations", org, &out); err != nil {
			return fmt.Errorf("register the fixture organisation: %w", err)
		}
		reg.State = out.Registration.State
	}
	if reg.State == "APPLIED" {
		if len(w.Terms) == 0 {
			return fmt.Errorf("no fixture terms to accept for the organisation")
		}
		t := w.Terms[0]
		if err := s.Parties.Post(ctx, "/v1/organisations/"+fixtures.OrgID+"/terms-acceptance",
			map[string]any{"termsId": t.ID, "termsVersion": t.Version, "acceptedBy": fixtures.OrgID},
			&reg); err != nil {
			return fmt.Errorf("accept terms for the fixture organisation: %w", err)
		}
	}
	if reg.State == "TERMS_ACCEPTED" {
		// The decision is the custodian's, and the decider's name is checked,
		// not just recorded — so the seeder binds a subject to the custodian
		// party the same bootstrap way it bound one to the organisation.
		token, err := oidc.Token(ctx, "seed|approver")
		if err != nil {
			return fmt.Errorf("mint approving token: %w", err)
		}
		asApprover := s.Parties.As(Caller{Token: token})
		if err := asApprover.Post(ctx, "/v1/parties/"+fixtures.CustodianID+"/identity-bindings",
			map[string]any{
				"provider":      "mock-oidc",
				"providerClass": "generic-oidc",
				"subjectRef":    oidc.Subject("seed|approver"),
			}, nil); err != nil {
			return fmt.Errorf("bind the approver to the custodian: %w", err)
		}
		if err := asApprover.Post(ctx, "/v1/organisations/"+fixtures.OrgID+"/decision",
			map[string]any{"approve": true, "decidedBy": fixtures.CustodianID}, &reg); err != nil {
			return fmt.Errorf("approve the fixture organisation: %w", err)
		}
	}
	if reg.State != "APPROVED" {
		return fmt.Errorf("the fixture organisation's registration is %s, not APPROVED", reg.State)
	}
	return nil
}
