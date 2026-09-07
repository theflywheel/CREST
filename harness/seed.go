package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	w.RuntimeIDs = make(map[string]string)
	manifest, err := loadRuntimeManifest()
	if err != nil {
		return nil, err
	}
	for fixtureID, runtimeID := range manifest {
		w.RuntimeIDs[fixtureID] = runtimeID
		s.SetRuntimeID(fixtureID, runtimeID)
	}

	if err := s.SetClock(ctx, w.Instance.Epoch); err != nil {
		return nil, fmt.Errorf("set clock: %w", err)
	}
	// Mutating reference data and definitions is an authenticated operation.
	// Keep these callers separate: the definition author and approver must be
	// real, differently bound identities so the seed exercises separation of
	// duties instead of putting the fixture's party ids in request bodies.
	oidc := NewOIDC()
	if err := oidc.WaitReady(ctx, 60*time.Second); err != nil {
		return nil, fmt.Errorf("identity provider: %w", err)
	}
	setupSub := env("CREST_SETUP_PROVIDER_SUBJECT", "seed|custodian")
	seedToken, err := oidc.Token(ctx, setupSub)
	if err != nil {
		return nil, fmt.Errorf("mint seeding token: %w", err)
	}
	authorToken, err := oidc.Token(ctx, "seed|specifier")
	if err != nil {
		return nil, fmt.Errorf("mint definition-author token: %w", err)
	}
	approverToken, err := oidc.Token(ctx, "seed|approver")
	if err != nil {
		return nil, fmt.Errorf("mint definition-approver token: %w", err)
	}
	asSeeder := s.Parties.As(Caller{Token: seedToken})
	if err := setupFixtureOperator(ctx, asSeeder); err != nil {
		return nil, err
	}
	// All non-operator parties are enrolled by the already approved operator.
	// The public party endpoint therefore creates them without an identity
	// binding, except for the author and approver who register with their own
	// mock OIDC sessions. No fixture id or binding is sent to the server.
	asAuthor := s.Definitions.As(Caller{Token: authorToken})
	asApprover := s.Definitions.As(Caller{Token: approverToken})
	for _, actor := range []struct{ id, token string }{
		{fixtures.SpecifierID, authorToken}, {fixtures.CustodianID, approverToken},
	} {
		if partyID, ok := authenticatedParty(ctx, s.Parties, actor.token); ok {
			if existing := w.ResolveID(actor.id); existing == actor.id {
				w.RuntimeIDs[actor.id] = partyID
				s.SetRuntimeID(actor.id, partyID)
			} else if existing != partyID {
				return nil, fmt.Errorf("stable actor %s is bound to %s, manifest says %s", actor.id, partyID, existing)
			}
		}
	}
	for _, p := range w.Parties {
		if p.ID == fixtures.OrgID {
			continue
		}
		if existing := w.RuntimeIDs[p.ID]; existing != "" {
			if err := verifyRuntimeParty(ctx, s.Parties, existing, p.DisplayName); err == nil {
				continue
			}
			delete(w.RuntimeIDs, p.ID)
		}
		// The operator is the inviter, so the caller already has a Party and
		// createParty deliberately leaves the new person unbound.
		partyCaller := asSeeder
		switch p.ID {
		case fixtures.SpecifierID:
			partyCaller = s.Parties.As(Caller{Token: authorToken})
		case fixtures.CustodianID:
			partyCaller = s.Parties.As(Caller{Token: approverToken})
		}
		var created schema.Party
		if err := partyCaller.Post(ctx, "/v1/parties", schema.Party{
			Kind: p.Kind, DisplayName: p.DisplayName, ContactRoutes: p.ContactRoutes,
			CreatedAt: p.CreatedAt,
		}, &created); err != nil {
			return nil, fmt.Errorf("party %s: %w", p.DisplayName, err)
		}
		if created.ID == "" {
			return nil, fmt.Errorf("party %s: registration returned no id", p.DisplayName)
		}
		s.SetRuntimeID(p.ID, created.ID)
		w.RuntimeIDs[p.ID] = created.ID
	}
	if err := saveRuntimeManifest(w.RuntimeIDs); err != nil {
		return nil, err
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
		if err := asSeeder.Post(ctx, "/v1/skills", sk, nil); err != nil {
			return nil, fmt.Errorf("skill %s: %w", sk.Code, err)
		}
	}
	// Terms are published by the instance operator established by the public
	// setup route above.
	for _, t := range w.Terms {
		var listed struct {
			Terms []schema.Terms `json:"terms"`
		}
		if err := s.Parties.Get(ctx, "/v1/terms", &listed); err != nil {
			return nil, fmt.Errorf("list terms: %w", err)
		}
		found := false
		for _, existing := range listed.Terms {
			if existing.ID == t.ID && existing.Version == t.Version {
				found = true
				break
			}
		}
		if !found {
			if err := asSeeder.Post(ctx, "/v1/terms", t, nil); err != nil {
				return nil, fmt.Errorf("terms %s: %w", t.Name, err)
			}
		}
	}
	// The operator first grants itself the instance permissions needed to create
	// the project. Context-scoped grants follow after the server has assigned the
	// project id.
	// The setup route records the operator's approval, so grants are still
	// created through the ordinary authorization API with the authenticated
	// operator as their actual approver.
	for _, a := range w.Authorizations {
		if a.Scope.Kind != schema.AuthorizationScopeKindInstance {
			continue
		}
		a = runtimeAuthorization(a, w)
		code, body, err := asSeeder.Status(ctx, http.MethodPost, "/v1/authorizations", a)
		if err != nil {
			return nil, fmt.Errorf("authorization %s: %w", a.ID, err)
		}
		switch {
		case code >= 200 && code < 300:
		case code == http.StatusConflict && bytes.Contains(body, []byte("grant_not_editable")):
			// A rebased world re-seeded over a stack that keeps its database:
			// the grant is already there, minted by an earlier seed whose
			// epoch — and therefore whose period timestamps — differ from
			// this run's, so it cannot be a byte-exact replay. The registry
			// is right to refuse the rewrite (a grant is narrowed by revoke
			// and re-grant, not edited), and the seeder is right to accept
			// the standing grant as the thing it came to create — but only
			// after checking it still IS that thing. A fixture whose
			// functions moved on from what the earlier seed granted would
			// otherwise pass silently, and every scenario built on the seed
			// would then run against permissions nobody chose.
			var standing schema.Authorization
			if err := asSeeder.Get(ctx, "/v1/authorizations/"+url.PathEscape(a.ID), &standing); err != nil {
				return nil, fmt.Errorf("authorization %s: read the standing grant: %w", a.ID, err)
			}
			// The whole document, not a field list: a grant document is
			// immutable, so the only differences a reseed may tolerate are
			// the ones the rebase itself moves — period, approvedAt,
			// reviewBy. Normalize those three to the fixture's values and
			// every remaining byte (terms, evidence, functions, scope,
			// state, revocation) must match exactly, or the seeder is
			// quietly adopting a grant nobody chose.
			normalized := standing
			normalized.Period = a.Period
			normalized.ApprovedAt = a.ApprovedAt
			normalized.ReviewBy = a.ReviewBy
			want, err := json.Marshal(fixtureAuthorization(a, w))
			if err != nil {
				return nil, fmt.Errorf("authorization %s: marshal fixture: %w", a.ID, err)
			}
			got, err := json.Marshal(normalized)
			if err != nil {
				return nil, fmt.Errorf("authorization %s: marshal standing grant: %w", a.ID, err)
			}
			if !bytes.Equal(want, got) {
				return nil, fmt.Errorf(
					"authorization %s: the standing grant is not the fixture's document (something beyond the rebased time fields drifted); revoke and re-grant, not reseed over it",
					a.ID)
			}
			log.Printf("seed: authorization %s already granted by an earlier seed with the fixture's exact shape; left as is", a.ID)
		default:
			return nil, fmt.Errorf("authorization %s: %d: %s", a.ID, code, body)
		}
	}
	// First-run setup approves the operator before any terms exist. That narrow
	// bootstrap is enough to publish terms and establish the initial instance
	// roles, but project grants and invitations correctly require a real terms
	// acceptance. Record it through the ordinary reviewed upgrade path before
	// creating any context-scoped grants.
	if err := ensureOperatorTerms(ctx, asSeeder,
		s.Parties.As(Caller{Token: approverToken}), w); err != nil {
		return nil, err
	}
	for _, c := range w.Contexts {
		if existing := w.RuntimeIDs[c.ID]; existing != "" {
			var view any
			if err := asSeeder.Get(ctx, "/v1/projects/"+url.PathEscape(existing), &view); err == nil {
				continue
			}
			delete(w.RuntimeIDs, c.ID)
		}
		request := c
		request.ID = ""
		request.OwnerPartyID = w.ResolveID(request.OwnerPartyID)
		request.State = schema.ContextStateDRAFT
		request.ActivationGates = append([]schema.ContextActivationGatesItem(nil), c.ActivationGates...)
		for i := range request.ActivationGates {
			request.ActivationGates[i].SatisfiedAt = nil
		}
		var created schema.Context
		if err := asSeeder.Post(ctx, "/v1/contexts", request, &created); err != nil {
			return nil, fmt.Errorf("context %s: %w", c.Name, err)
		}
		if created.ID == "" {
			return nil, fmt.Errorf("context %s: response returned no id", c.Name)
		}
		s.SetRuntimeID(c.ID, created.ID)
		w.RuntimeIDs[c.ID] = created.ID
	}
	if err := saveRuntimeManifest(w.RuntimeIDs); err != nil {
		return nil, err
	}
	for _, a := range w.Authorizations {
		if a.Scope.Kind != schema.AuthorizationScopeKindContext {
			continue
		}
		a = runtimeAuthorization(a, w)
		code, body, err := asSeeder.Status(ctx, http.MethodPost, "/v1/authorizations", a)
		if err != nil {
			return nil, fmt.Errorf("authorization %s: %w", a.ID, err)
		}
		if code >= 200 && code < 300 {
			continue
		}
		if code == http.StatusConflict {
			var standing schema.Authorization
			if err := asSeeder.Get(ctx, "/v1/authorizations/"+url.PathEscape(a.ID), &standing); err == nil {
				if authorizationMatchesFixture(a, standing, w) {
					continue
				}
				return nil, fmt.Errorf("authorization %s: standing grant differs from fixture", a.ID)
			}
		}
		return nil, fmt.Errorf("authorization %s: %d: %s", a.ID, code, body)
	}
	for _, c := range w.Contexts {
		id := w.ResolveID(c.ID)
		for _, gate := range c.ActivationGates {
			if err := asSeeder.Post(ctx, "/v1/projects/"+url.PathEscape(id)+"/activation/gates/"+url.PathEscape(gate.Name)+"/satisfied", nil, nil); err != nil {
				return nil, fmt.Errorf("satisfy context %s gate %s: %w", c.Name, gate.Name, err)
			}
		}
		if err := asSeeder.Post(ctx, "/v1/projects/"+url.PathEscape(id)+"/activation", nil, nil); err != nil {
			return nil, fmt.Errorf("activate context %s: %w", c.Name, err)
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
	if err := s.Verification.Get(ctx, "/v1/issuer", &issuer); err != nil {
		return nil, fmt.Errorf("ask the verification service which issuer it signs with: %w", err)
	}
	if issuer.Issuer == "" {
		return nil, fmt.Errorf("the verification service named no issuer; a definition cannot authorise one")
	}

	for _, d := range w.Definitions {
		var existing schema.Definition
		if err := s.Definitions.Get(ctx, "/v1/definitions/"+d.ID, &existing); err == nil &&
			existing.State == schema.DefinitionStateACTIVE {
			continue
		}
		ratifier := d.RatifiedByPartyID
		draft := runtimeDefinition(d, w)
		draft.Faces.Verifier.AuthorisedIssuers = withIssuer(
			d.Faces.Verifier.AuthorisedIssuers, issuer.Issuer)
		draft.State = schema.DefinitionStateDRAFT
		draft.RatifiedByPartyID = nil
		draft.ActivatedAt = nil
		if err := asAuthor.Post(ctx, "/v1/definitions", draft, nil); err != nil {
			return nil, fmt.Errorf("definition %s: %w", d.ID, err)
		}
		if ratifier == nil {
			return nil, fmt.Errorf("definition %s has no ratifier in the fixture world", d.ID)
		}
		path := fmt.Sprintf("/v1/definitions/%s/versions/%d", d.ID, d.Version)
		if err := asApprover.Post(ctx, path+"/ratify",
			map[string]any{"ratifiedByPartyId": w.ResolveID(*ratifier)}, nil); err != nil {
			return nil, fmt.Errorf("ratify %s: %w", d.ID, err)
		}
		if err := asApprover.Post(ctx, path+"/activate",
			map[string]any{"activatedByPartyId": w.ResolveID(*ratifier)}, nil); err != nil {
			return nil, fmt.Errorf("activate %s: %w", d.ID, err)
		}
	}

	for _, lr := range w.LinkedRecords {
		if lr.KeyedTo.Kind != schema.LinkedRecordKeyedToKindDefinition {
			continue
		}
		linked := lr
		if payload, ok := remapJSONValue(linked.Payload, w).(map[string]any); ok {
			linked.Payload = payload
		}
		code, body, err := asAuthor.Status(ctx, http.MethodPost,
			"/v1/definitions/"+w.ResolveID(lr.KeyedTo.ID)+"/linked-records", linked)
		if err != nil {
			return nil, fmt.Errorf("linked record %s: %w", lr.ID, err)
		}
		if code < 200 || code >= 300 {
			if code == http.StatusConflict {
				continue
			}
			return nil, fmt.Errorf("linked record %s: %d: %s", lr.ID, code, body)
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

func setupFixtureOperator(ctx context.Context, operator *Service) error {
	body := map[string]any{
		"displayName":   "Riverside Health Programme",
		"contactRoutes": []map[string]any{{"kind": "email", "value": "programme@riverside.invalid"}},
	}
	code, raw, err := operator.Status(ctx, http.MethodPost, "/v1/instance/setup", body)
	if err != nil {
		return fmt.Errorf("instance setup request: %w", err)
	}
	if code == http.StatusConflict || code == http.StatusForbidden {
		partyID, ok := authenticatedParty(ctx, operator, "")
		if ok && partyID == fixtures.OrgID {
			return nil
		}
		return fmt.Errorf("instance setup was refused with %d and the caller is not the configured operator", code)
	}
	if code != http.StatusCreated {
		return fmt.Errorf("instance setup: %d: %s (set CREST_SETUP_PROVIDER_SUBJECT to the configured administrator subject)", code, raw)
	}
	var out struct {
		Party schema.Party `json:"party"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode instance setup: %w", err)
	}
	if out.Party.ID != fixtures.OrgID {
		return fmt.Errorf("instance setup returned operator %q, want configured fixture operator %q", out.Party.ID, fixtures.OrgID)
	}
	return nil
}

func ensureOperatorTerms(ctx context.Context, operator, reviewer *Service, w *fixtures.World) error {
	if len(w.Terms) == 0 {
		return fmt.Errorf("fixture world has no terms for its operator to accept")
	}
	want := w.Terms[0]
	var reg struct {
		TermsID      string `json:"termsId"`
		TermsVersion int    `json:"termsVersion"`
	}
	if err := operator.Get(ctx, "/v1/organisations/"+url.PathEscape(fixtures.OrgID)+"/registration", &reg); err != nil {
		return fmt.Errorf("read fixture operator registration: %w", err)
	}
	if reg.TermsID != "" || reg.TermsVersion != 0 {
		if reg.TermsID == want.ID && reg.TermsVersion == want.Version {
			return nil
		}
		return fmt.Errorf("fixture operator holds terms %s v%d, want %s v%d",
			reg.TermsID, reg.TermsVersion, want.ID, want.Version)
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := operator.Post(ctx, "/v1/organisations/"+url.PathEscape(fixtures.OrgID)+"/terms-requests",
		map[string]any{"termsId": want.ID, "termsVersion": want.Version, "documents": []any{}}, &req); err != nil {
		return fmt.Errorf("request fixture operator terms: %w", err)
	}
	if req.ID == "" {
		return fmt.Errorf("request fixture operator terms: response returned no id")
	}
	path := "/v1/terms-requests/" + url.PathEscape(req.ID)
	if err := operator.Post(ctx, path+"/submit", nil, nil); err != nil {
		return fmt.Errorf("submit fixture operator terms request: %w", err)
	}
	reviewerID := w.ResolveID(fixtures.CustodianID)
	if err := reviewer.Post(ctx, path+"/checks", map[string]any{
		"name": "fixture-review", "outcome": "PASS", "ownerKind": "party",
		"owner": reviewerID, "recordedBy": reviewerID,
		"note": "canonical fixture terms reviewed before project grants",
	}, nil); err != nil {
		return fmt.Errorf("review fixture operator terms request: %w", err)
	}
	if err := reviewer.Post(ctx, path+"/decision",
		map[string]any{"approve": true, "decidedBy": reviewerID}, nil); err != nil {
		return fmt.Errorf("approve fixture operator terms request: %w", err)
	}
	return nil
}

func authenticatedParty(ctx context.Context, parties *Service, token string) (string, bool) {
	view := parties
	if token != "" {
		view = parties.As(Caller{Token: token})
	}
	code, raw, err := view.Status(ctx, http.MethodGet, "/v1/auth/me", nil)
	if err != nil || code != http.StatusOK {
		return "", false
	}
	var out struct {
		PartyID string `json:"partyId"`
	}
	if json.Unmarshal(raw, &out) != nil || out.PartyID == "" {
		return "", false
	}
	return out.PartyID, true
}

func verifyRuntimeParty(ctx context.Context, parties *Service, id, displayName string) error {
	// Service.Get reverses runtime ids back to fixture aliases for scenario
	// assertions. This check is validating the persisted runtime id itself, so
	// decode the raw response and avoid adopting an alias as if it were a
	// missing party on the next seed pass.
	code, raw, err := parties.Status(ctx, http.MethodGet, "/internal/parties/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if code < 200 || code >= 300 {
		return fmt.Errorf("runtime party lookup: %d: %s", code, raw)
	}
	var p schema.Party
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("decode runtime party: %w", err)
	}
	if p.ID != id || p.DisplayName != displayName {
		return fmt.Errorf("runtime party %s does not match fixture party %q", id, displayName)
	}
	return nil
}

func runtimeManifestPath() string {
	if configured := os.Getenv("CREST_HARNESS_RUNTIME_MANIFEST"); configured != "" {
		return configured
	}
	instance := env("CREST_INSTANCE_ID", "crest:instance:local")
	sum := sha256.Sum256([]byte(instance))
	return filepath.Join(os.TempDir(), "crest-harness-runtime-"+hex.EncodeToString(sum[:8])+".json")
}

func loadRuntimeManifest() (map[string]string, error) {
	raw, err := os.ReadFile(runtimeManifestPath())
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read harness runtime manifest: %w", err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("decode harness runtime manifest: %w", err)
	}
	if manifest == nil {
		manifest = map[string]string{}
	}
	return manifest, nil
}

func saveRuntimeManifest(manifest map[string]string) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode harness runtime manifest: %w", err)
	}
	path := runtimeManifestPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write harness runtime manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install harness runtime manifest: %w", err)
	}
	return nil
}

func runtimeAuthorization(a schema.Authorization, w *fixtures.World) schema.Authorization {
	if a.AuthorityPartyID != "" {
		a.AuthorityPartyID = w.ResolveID(a.AuthorityPartyID)
	}
	if a.ApprovedByPartyID != "" {
		// The authenticated setup operator is the actual approver of these
		// initial grants; a fixture body cannot assert a different person.
		a.ApprovedByPartyID = w.ResolveID(fixtures.OrgID)
	}
	a.PartyID = w.ResolveID(a.PartyID)
	if a.Scope.ContextID != nil {
		id := w.ResolveID(*a.Scope.ContextID)
		a.Scope.ContextID = &id
	}
	return a
}

func fixtureAuthorization(a schema.Authorization, w *fixtures.World) schema.Authorization {
	a.AuthorityPartyID = fixtureID(w, a.AuthorityPartyID)
	a.ApprovedByPartyID = fixtureID(w, a.ApprovedByPartyID)
	a.PartyID = fixtureID(w, a.PartyID)
	if a.Scope.ContextID != nil {
		id := fixtureID(w, *a.Scope.ContextID)
		a.Scope.ContextID = &id
	}
	return a
}

func authorizationMatchesFixture(want, standing schema.Authorization, w *fixtures.World) bool {
	normalized := standing
	normalized.Period = want.Period
	normalized.ApprovedAt = want.ApprovedAt
	normalized.ReviewBy = want.ReviewBy
	want = fixtureAuthorization(want, w)
	left, err := json.Marshal(want)
	if err != nil {
		return false
	}
	right, err := json.Marshal(normalized)
	return err == nil && bytes.Equal(left, right)
}

func fixtureID(w *fixtures.World, runtimeID string) string {
	for fixture, runtime := range w.RuntimeIDs {
		if runtime == runtimeID {
			return fixture
		}
	}
	return runtimeID
}

func runtimeDefinition(d schema.Definition, w *fixtures.World) schema.Definition {
	d.TierMap = append([]schema.DefinitionTierMapItem(nil), d.TierMap...)
	d.AuthoredByPartyID = w.ResolveID(d.AuthoredByPartyID)
	if d.ContextID != nil {
		id := w.ResolveID(*d.ContextID)
		d.ContextID = &id
	}
	if d.RatifiedByPartyID != nil {
		id := w.ResolveID(*d.RatifiedByPartyID)
		d.RatifiedByPartyID = &id
	}
	// Fixture definitions predate reference-v1's public tier ordering. Convert
	// the immutable fixture shape in the request copy only; the source fixture
	// remains legacy-v0 for strength regression tests.
	semantics := schema.DefinitionTierSemanticsReferenceV1
	d.TierSemantics = &semantics
	if d.Faces.Worker.TierCeiling > 0 {
		d.Faces.Worker.TierCeiling = 4 - d.Faces.Worker.TierCeiling
	}
	for i := range d.TierMap {
		if d.TierMap[i].Tier > 0 && d.TierMap[i].Tier < 4 {
			d.TierMap[i].Tier = 4 - d.TierMap[i].Tier
		}
	}
	return d
}

func remapJSONValue(value any, w *fixtures.World) any {
	switch v := value.(type) {
	case string:
		return w.ResolveID(v)
	case []any:
		for i := range v {
			v[i] = remapJSONValue(v[i], w)
		}
	case map[string]any:
		for key, child := range v {
			v[key] = remapJSONValue(child, w)
		}
	}
	return value
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
