package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/schema"
)

// The chain above the definition, link by link (#27). Every way a link can
// fail to bear out the credential's own account of itself is a rejection —
// a credential whose provenance story is false is not a weaker credential.

const (
	walkOrg     = "did:crest:party:01TESTORG000000000000000000"
	walkQual    = "crest:authorization:01TESTQUAL0000000000000000"
	walkGrant   = "crest:authorization:01TESTGRANT000000000000000"
	walkProject = "crest:context:01TESTPROJECT000000000000"
	walkIssuer  = "did:crest:issuer:walk"
)

var (
	walkIssuedAt = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	walkStart    = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func str(s string) *string      { return &s }
func at(t time.Time) *time.Time { return &t }

func TestABrokenAuthorizationLinkIsNamedForWhatItIs(t *testing.T) {
	good := authorizationFace{
		ID: walkGrant, PartyID: walkOrg, Functions: []string{"submit-work-evidence"},
		Scope:  schema.AuthorizationScope{Kind: schema.AuthorizationScopeKindContext, ContextID: str(walkProject)},
		Period: schema.Period{Start: walkStart}, State: "ACTIVE",
	}
	want := authorizationWant{
		Ref: walkGrant, OrgID: walkOrg, Scope: schema.AuthorizationScopeKindContext,
		ContextID: str(walkProject), At: walkIssuedAt,
	}
	if err := checkAuthorization(good, want); err != nil {
		t.Fatalf("a sound link was rejected: %v", err)
	}

	cases := []struct {
		name string
		face func(authorizationFace) authorizationFace
		says string
	}{
		{"held by another party", func(f authorizationFace) authorizationFace { f.PartyID = "did:crest:party:someone-else"; return f }, "not by the organisation the credential names"},
		{"wrong scope", func(f authorizationFace) authorizationFace {
			f.Scope = schema.AuthorizationScope{Kind: schema.AuthorizationScopeKindInstance}
			return f
		}, "instance-scoped"},
		{"a grant for another project", func(f authorizationFace) authorizationFace { f.Scope.ContextID = str("crest:context:other"); return f }, "not for the project"},
		{"not yet in force at issuance", func(f authorizationFace) authorizationFace { f.Period.Start = walkIssuedAt.Add(time.Hour); return f }, "came into force"},
		{"expired before issuance", func(f authorizationFace) authorizationFace { f.Period.End = at(walkIssuedAt.Add(-time.Hour)); return f }, "expired"},
		{"revoked before issuance", func(f authorizationFace) authorizationFace {
			f.RevokedAt = at(walkIssuedAt.Add(-time.Minute))
			f.State = "REVOKED"
			return f
		}, "was revoked"},
		{"never active", func(f authorizationFace) authorizationFace { f.State = "PENDING"; return f }, "not active"},
		{"the registry answered a different record", func(f authorizationFace) authorizationFace { f.ID = "crest:authorization:another"; return f }, "answered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAuthorization(tc.face(good), want)
			if err == nil {
				t.Fatal("a broken link was accepted")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the rejection does not say why: %q lacks %q", err, tc.says)
			}
		})
	}

	// Revoked AFTER issuance is the one that is not a rejection: the
	// attestation stood when it was made. It is reported, not refused.
	later := good
	later.RevokedAt = at(walkIssuedAt.Add(24 * time.Hour))
	later.State = "REVOKED"
	if err := checkAuthorization(later, want); err != nil {
		t.Errorf("a revocation after issuance rejected the credential: %v", err)
	}
	if revokedSince(later, walkIssuedAt) == nil {
		t.Error("a revocation after issuance went unreported")
	}
	if revokedSince(good, walkIssuedAt) != nil {
		t.Error("an unrevoked authorization was reported revoked")
	}
}

func TestAnAttesterFunctionMustBeHeldByOneOfTheNamedAuthorizations(t *testing.T) {
	def := schema.Definition{AuthorisedAttesterFunctions: []string{"submit-work-evidence"}}
	qual := authorizationFace{Functions: []string{"attest-work"}}
	grant := authorizationFace{Functions: []string{"attest-work", "submit-work-evidence"}}
	if !attesterFunctionHeld(def, qual, grant) {
		t.Error("the grant carries the function and the chain was refused")
	}
	if attesterFunctionHeld(def, qual) {
		t.Error("no named authorization carries the function and the chain was accepted")
	}
	if !attesterFunctionHeld(schema.Definition{}, qual) {
		t.Error("a definition that names no attester functions must accept any")
	}
}

// fakeRegistry is the parties service as the walk sees it: publications with
// their faces, and the deployment's self-description.
type fakeRegistry struct {
	facts    map[string]map[string]any // "kind/id" → face
	instance map[string]any
	// transparent flags every publication as log-backed.
	transparent bool
}

func (f *fakeRegistry) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/instance" {
			if f.instance == nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"code":"no_instance_identity"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(f.instance)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/publications/")
		face, ok := f.facts[rest]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"not_published"}`))
			return
		}
		kind := strings.SplitN(rest, "/", 2)[0]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"namespace": "crest", "registry": kind + "s", "record": strings.SplitN(rest, "/", 2)[1],
			"registryVersion": "v3", "transparent": f.transparent, "face": face,
		})
	}))
}

func soundRegistry() *fakeRegistry {
	return &fakeRegistry{
		facts: map[string]map[string]any{
			"authorization/" + walkQual: {
				"authorizationId": walkQual, "partyId": walkOrg, "functions": []string{"attest-work"},
				"scope": map[string]any{"kind": "instance"}, "period": map[string]any{"start": walkStart},
				"state": "ACTIVE",
			},
			"authorization/" + walkGrant: {
				"authorizationId": walkGrant, "partyId": walkOrg, "functions": []string{"attest-work", "submit-work-evidence"},
				"scope": map[string]any{"kind": "context", "contextId": walkProject}, "period": map[string]any{"start": walkStart},
				"state": "ACTIVE",
			},
			"organisation/" + walkOrg: {"partyId": walkOrg, "kind": "organisation", "displayName": "Riverside Health"},
		},
		instance: map[string]any{
			"instance": map[string]any{"instanceId": "crest:instance:test", "operatorPartyId": walkOrg, "issuerId": walkIssuer},
			"publication": map[string]any{"namespace": "crest", "registry": "instances", "record": "crest:instance:test",
				"registryVersion": "v1", "transparent": true},
		},
		transparent: true,
	}
}

func walkHandlers(t *testing.T, registry *httptest.Server, dediURL string) *handlers {
	t.Helper()
	issuer, err := credential.NewIssuer(walkIssuer, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return &handlers{
		issuer: issuer, registry: client.New(registry.URL), dediURL: dediURL,
		trustedIssuers: map[string]trustedIssuer{walkIssuer: {}},
	}
}

func walkCredential() schema.WorkEventCredential {
	var cred schema.WorkEventCredential
	cred.ValidFrom = walkIssuedAt
	cred.CredentialSubject.IssuerAuthority = &schema.WorkEventCredentialCredentialSubjectIssuerAuthority{
		OrgID: walkOrg, QualificationRef: str(walkQual), GrantRef: str(walkGrant),
	}
	return cred
}

func walkDefinition() schema.Definition {
	return schema.Definition{ID: "crest:definition:D", Version: 2, ContextID: str(walkProject),
		AuthorisedAttesterFunctions: []string{"submit-work-evidence"}}
}

func TestASoundChainWalksToTheDeploymentAndSaysWhereToCheck(t *testing.T) {
	srv := soundRegistry().serve(t)
	defer srv.Close()
	h := walkHandlers(t, srv, "https://dedi.example")

	walk := h.authorityChain(context.Background(), walkIssuer, walkCredential(), walkDefinition())
	if len(walk.Broken) > 0 {
		t.Fatalf("a sound chain was rejected: %v", walk.Broken)
	}
	if len(walk.Links) != 4 {
		t.Fatalf("want four links — qualification, grant, organisation, instance — got %d: %+v", len(walk.Links), walk.Links)
	}
	for _, l := range walk.Links {
		if !l.Checkable || !strings.Contains(l.How, "https://dedi.example/dedi/lookup/crest/") {
			t.Errorf("a log-backed fact was not handed to the verifier as checkable: %+v", l)
		}
	}
	if len(walk.NotEstablished) != 0 {
		t.Errorf("a complete chain left something not established: %v", walk.NotEstablished)
	}

	// The same facts on a registry with no log behind it: every link still
	// holds, and every link says the verifier is taking the deployment's word.
	plain := soundRegistry()
	plain.transparent = false
	plain.instance["publication"].(map[string]any)["transparent"] = false
	srv2 := plain.serve(t)
	defer srv2.Close()
	walk = walkHandlers(t, srv2, "").authorityChain(context.Background(), walkIssuer, walkCredential(), walkDefinition())
	if len(walk.Broken) > 0 {
		t.Fatalf("the fallback registry's sound chain was rejected: %v", walk.Broken)
	}
	for _, l := range walk.Links {
		if l.Checkable || l.Trusting == "" {
			t.Errorf("a fallback-registry fact was presented as independently checkable: %+v", l)
		}
	}
}

func TestEachBrokenLinkRejectsTheCredential(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fakeRegistry)
		says   string
	}{
		{"the qualification is not published", func(f *fakeRegistry) { delete(f.facts, "authorization/"+walkQual) }, "not in the authorizations registry"},
		{"the grant belongs to another organisation", func(f *fakeRegistry) {
			f.facts["authorization/"+walkGrant]["partyId"] = "did:crest:party:impostor"
		}, "not by the organisation the credential names"},
		{"the grant is for another project", func(f *fakeRegistry) {
			f.facts["authorization/"+walkGrant]["scope"] = map[string]any{"kind": "context", "contextId": "crest:context:elsewhere"}
		}, "not for the project"},
		{"the qualification was revoked before issuance", func(f *fakeRegistry) {
			f.facts["authorization/"+walkQual]["revokedAt"] = walkIssuedAt.Add(-time.Hour)
			f.facts["authorization/"+walkQual]["state"] = "REVOKED"
		}, "was revoked"},
		{"no named authorization may attest under this definition", func(f *fakeRegistry) {
			f.facts["authorization/"+walkGrant]["functions"] = []string{"attest-work"}
		}, "accepts for attesting"},
		{"the organisation is not in the registry", func(f *fakeRegistry) { delete(f.facts, "organisation/"+walkOrg) }, "not in the organisations registry"},
		{"the registry's record of the organisation is a person's", func(f *fakeRegistry) {
			f.facts["organisation/"+walkOrg]["kind"] = "person"
		}, "not an organisation's"},
		{"the deployment is operated by somebody else", func(f *fakeRegistry) {
			f.instance["instance"].(map[string]any)["operatorPartyId"] = "did:crest:party:ministry"
		}, "operated by"},
		{"the deployment issues under a different DID", func(f *fakeRegistry) {
			f.instance["instance"].(map[string]any)["issuerId"] = "did:crest:issuer:other"
		}, "issues as"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := soundRegistry()
			tc.mutate(reg)
			srv := reg.serve(t)
			defer srv.Close()
			walk := walkHandlers(t, srv, "https://dedi.example").
				authorityChain(context.Background(), walkIssuer, walkCredential(), walkDefinition())
			if len(walk.Broken) == 0 {
				t.Fatalf("the chain was accepted with %s", tc.name)
			}
			if !strings.Contains(strings.Join(walk.Broken, "\n"), tc.says) {
				t.Errorf("the rejection does not say why: %q lacks %q", walk.Broken, tc.says)
			}
		})
	}
}

func TestAnAbsentChainIsDisclosedNotRejected(t *testing.T) {
	srv := soundRegistry().serve(t)
	defer srv.Close()
	h := walkHandlers(t, srv, "https://dedi.example")

	// No issuerAuthority at all: nothing false was said, so nothing is broken,
	// and the verdict says what it therefore cannot establish.
	var bare schema.WorkEventCredential
	bare.ValidFrom = walkIssuedAt
	walk := h.authorityChain(context.Background(), walkIssuer, bare, walkDefinition())
	if len(walk.Broken) > 0 || len(walk.Links) > 0 {
		t.Fatalf("a credential naming no authority was walked anyway: %+v", walk)
	}
	if len(walk.NotEstablished) != 1 || !strings.Contains(walk.NotEstablished[0], "names no issuer authority") {
		t.Errorf("the absence was not disclosed: %v", walk.NotEstablished)
	}

	// An organisation named with no refs: the organisation and instance links
	// are still walked, the missing authorizations are disclosed.
	cred := walkCredential()
	cred.CredentialSubject.IssuerAuthority.QualificationRef = nil
	cred.CredentialSubject.IssuerAuthority.GrantRef = nil
	walk = h.authorityChain(context.Background(), walkIssuer, cred, walkDefinition())
	if len(walk.Broken) > 0 {
		t.Fatalf("an organisation with no refs was rejected: %v", walk.Broken)
	}
	if len(walk.Links) != 2 {
		t.Errorf("want the organisation and instance links only, got %+v", walk.Links)
	}
	if len(walk.NotEstablished) != 2 {
		t.Errorf("both missing authorizations should be disclosed, got %v", walk.NotEstablished)
	}

	// A revocation after issuance: valid, and said.
	reg := soundRegistry()
	reg.facts["authorization/"+walkGrant]["revokedAt"] = walkIssuedAt.Add(48 * time.Hour)
	reg.facts["authorization/"+walkGrant]["state"] = "REVOKED"
	srv2 := reg.serve(t)
	defer srv2.Close()
	walk = walkHandlers(t, srv2, "").authorityChain(context.Background(), walkIssuer, walkCredential(), walkDefinition())
	if len(walk.Broken) > 0 {
		t.Fatalf("a revocation after issuance rejected the credential: %v", walk.Broken)
	}
	if !strings.Contains(strings.Join(walk.NotEstablished, "\n"), "still holds") {
		t.Errorf("the later revocation was not disclosed: %v", walk.NotEstablished)
	}
}

// A credential from a trusted external issuer whose registry this deployment
// was not told about: the chain cannot be walked from here, and the verdict
// says so instead of pretending, and instead of rejecting.
func TestAnExternalIssuerWithoutARegistryIsNotWalked(t *testing.T) {
	srv := soundRegistry().serve(t)
	defer srv.Close()
	h := walkHandlers(t, srv, "")
	h.trustedIssuers["did:crest:issuer:elsewhere"] = trustedIssuer{Keys: map[string]string{"did:crest:issuer:elsewhere#key-1": "z"}}

	walk := h.authorityChain(context.Background(), "did:crest:issuer:elsewhere", walkCredential(), walkDefinition())
	if len(walk.Broken) > 0 {
		t.Fatalf("an unwalkable chain was rejected: %v", walk.Broken)
	}
	if len(walk.Links) != 1 || walk.Links[0].Checkable {
		t.Errorf("want one asserted link naming the credential's own word, got %+v", walk.Links)
	}
	if !strings.Contains(strings.Join(walk.NotEstablished, "\n"), "registry is not configured") {
		t.Errorf("the limit was not disclosed: %v", walk.NotEstablished)
	}

	// Told where that registry is, the walk proceeds there.
	h.trustedIssuers["did:crest:issuer:elsewhere"] = trustedIssuer{
		Keys: map[string]string{"did:crest:issuer:elsewhere#key-1": "z"}, Registry: srv.URL,
	}
	walk = h.authorityChain(context.Background(), "did:crest:issuer:elsewhere", walkCredential(), walkDefinition())
	// The fake instance issues as walkIssuer, so the last link breaks — which
	// is the walk happening, and the right answer for a deployment that says
	// it issues under one DID while the credential carries another.
	if !strings.Contains(strings.Join(walk.Broken, "\n"), "issues as") {
		t.Errorf("the external registry was not walked: broken=%v links=%+v", walk.Broken, walk.Links)
	}
}
