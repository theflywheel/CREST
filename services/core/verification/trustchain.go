package verification

// The chain above the definition (#27, Blueprint §3 and §8): from the
// credential's issuerAuthority up to the deployment that stands behind it.
//
// A credential names three things about who stands behind it — the
// organisation (orgId), that organisation's instance-wide authorization to
// attest at all (qualificationRef), and its context-bound one for this project
// (grantRef). Each is a published fact: organisations and organisations'
// authorizations go to the registry (§3), and the deployment's own
// self-description does too (#70). So a verifier can walk the whole chain
// without asking this service to confirm itself. This file is that walk, done on
// the verifier's behalf and reported link by link so they can repeat it.
//
// Broken at any link, the verdict is not valid. An authorization the
// organisation does not hold, a grant for some other project, an authorization
// that was not active when the credential was issued, an operator other than
// the organisation named — none of those is a weaker chain. Each is a credential
// whose own account of its provenance is false, and a signature over a false
// statement proves only that somebody signed it.
//
// A chain that is simply absent is different and is treated differently.
// Issuance resolves the authority best-effort, because it sits on the path that
// releases payment, so a credential may carry no issuerAuthority at all, or an
// orgId with no refs. That is a disclosed limit, reported under notEstablished,
// not a broken link — the credential has not said anything false, it has said
// less.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/schema"
)

// chainWalk is what the walk reports back into the verdict.
type chainWalk struct {
	Links []Link
	// Broken holds the reasons the verdict is not valid. Empty when every
	// link the credential names holds.
	Broken []string
	// NotEstablished holds what a valid verdict still does not prove about
	// the chain: links the credential did not name, and facts that have
	// changed since issuance.
	NotEstablished []string
}

// published is one registry fact as the parties service reports it: where it
// went, and what went there.
type published struct {
	Namespace       string         `json:"namespace"`
	Registry        string         `json:"registry"`
	Record          string         `json:"record"`
	RegistryVersion string         `json:"registryVersion"`
	Transparent     bool           `json:"transparent"`
	Face            map[string]any `json:"face"`
}

// authorizationFace is the published projection of an authorization, read
// back. The field names are the registry's, not the primitive's.
type authorizationFace struct {
	ID        string                    `json:"authorizationId"`
	PartyID   string                    `json:"partyId"`
	Functions []string                  `json:"functions"`
	Scope     schema.AuthorizationScope `json:"scope"`
	Period    schema.Period             `json:"period"`
	State     string                    `json:"state"`
	RevokedAt *time.Time                `json:"revokedAt,omitempty"`
}

// authorizationWant is what the credential's chain claims about an
// authorization, checked against the face the registry holds.
type authorizationWant struct {
	Ref   string
	OrgID string
	Scope schema.AuthorizationScopeKind
	// ContextID is the project the grant must be for, when the walk knows it.
	// Nil means the credential's definition is not bound to a project, and
	// the grant's own context cannot be checked against anything.
	ContextID *string
	// At is the instant the authorization had to be in force: the moment the
	// credential was issued. An authorization revoked afterwards does not
	// unmake the attestation; one revoked before it does.
	At time.Time
}

var errNotPublished = errors.New("not published")

// checkAuthorization is the pure half of one link: does the face the registry
// holds bear out what the credential claims? A nil error is a link that holds.
func checkAuthorization(face authorizationFace, want authorizationWant) error {
	if face.ID != "" && face.ID != want.Ref {
		return fmt.Errorf("the registry answered %s for %s", face.ID, want.Ref)
	}
	if face.PartyID != want.OrgID {
		return fmt.Errorf("%s is held by %s, not by the organisation the credential names (%s)",
			want.Ref, face.PartyID, want.OrgID)
	}
	if face.Scope.Kind != want.Scope {
		return fmt.Errorf("%s is %s-scoped where the credential names it as its %s authorization",
			want.Ref, face.Scope.Kind, want.Scope)
	}
	if want.Scope == schema.AuthorizationScopeKindContext && want.ContextID != nil {
		if face.Scope.ContextID == nil || *face.Scope.ContextID != *want.ContextID {
			got := "no project"
			if face.Scope.ContextID != nil {
				got = *face.Scope.ContextID
			}
			return fmt.Errorf("%s is a grant for %s, not for the project this work was defined under (%s)",
				want.Ref, got, *want.ContextID)
		}
	}
	if want.At.Before(face.Period.Start) {
		return fmt.Errorf("%s came into force on %s, after this credential was issued on %s",
			want.Ref, face.Period.Start.UTC().Format(time.RFC3339), want.At.UTC().Format(time.RFC3339))
	}
	if face.Period.End != nil && want.At.After(*face.Period.End) {
		return fmt.Errorf("%s expired on %s, before this credential was issued on %s",
			want.Ref, face.Period.End.UTC().Format(time.RFC3339), want.At.UTC().Format(time.RFC3339))
	}
	if face.RevokedAt != nil && !want.At.Before(*face.RevokedAt) {
		return fmt.Errorf("%s was revoked on %s, before this credential was issued on %s",
			want.Ref, face.RevokedAt.UTC().Format(time.RFC3339), want.At.UTC().Format(time.RFC3339))
	}
	if face.RevokedAt == nil && face.State != string(schema.AuthorizationStateACTIVE) {
		return fmt.Errorf("%s is %s, not active", want.Ref, strings.ToLower(face.State))
	}
	return nil
}

// revokedSince reports a revocation that happened after issuance: the link
// held when it mattered, and the verifier should still know.
func revokedSince(face authorizationFace, at time.Time) *time.Time {
	if face.RevokedAt != nil && at.Before(*face.RevokedAt) {
		return face.RevokedAt
	}
	return nil
}

// attesterFunctionHeld reports whether any of the authorizations the credential
// names carries a function the definition accepts for attesting. A definition
// that names none accepts any.
func attesterFunctionHeld(def schema.Definition, faces ...authorizationFace) bool {
	if len(def.AuthorisedAttesterFunctions) == 0 {
		return true
	}
	for _, f := range faces {
		for _, held := range f.Functions {
			for _, wanted := range def.AuthorisedAttesterFunctions {
				if held == wanted {
					return true
				}
			}
		}
	}
	return false
}

// registryReader is where the walk reads published facts from: this
// deployment's own registry service for its own credentials, or the registry a
// trusted external issuer was configured with.
type registryReader struct {
	get     func(ctx context.Context, path string, out any) error
	dediURL string
	// whose names the deployment being read, for the sentences.
	whose string
}

func (h *handlers) localReader() registryReader {
	return registryReader{get: h.registry.Get, dediURL: h.dediURL, whose: "this deployment"}
}

func (h *handlers) readerFor(issuerID string) (registryReader, bool) {
	if issuerID == h.issuer.ID() {
		return h.localReader(), true
	}
	trusted, ok := h.trustedIssuers[issuerID]
	if !ok || trusted.Registry == "" {
		return registryReader{}, false
	}
	c := client.New(trusted.Registry)
	return registryReader{get: c.Get, dediURL: trusted.DeDi, whose: issuerID + "'s deployment"}, true
}

func (r registryReader) publication(ctx context.Context, kind, id string) (published, error) {
	var out published
	err := r.get(ctx, fmt.Sprintf("/v1/publications/%s/%s", kind, url.PathEscape(id)), &out)
	var status *client.Status
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		return published{}, errNotPublished
	}
	return out, err
}

// instanceView is the deployment's self-description as /v1/instance answers it.
type instanceView struct {
	Instance struct {
		ID              string `json:"instanceId"`
		OperatorPartyID string `json:"operatorPartyId"`
		IssuerID        string `json:"issuerId"`
	} `json:"instance"`
	Publication *published `json:"publication,omitempty"`
}

// factLink says whether the verifier can check a published fact without
// taking the deployment's word for it, and where.
func (r registryReader) factLink(claim string, pub published) Link {
	switch {
	case !pub.Transparent:
		return asserted(claim, r.whose+"'s registry, which runs without a transparency log, so its copy proves nothing a verifier can check independently")
	case r.dediURL == "":
		return asserted(claim, r.whose+"'s registry; it does not publish where its node can be reached")
	}
	return checkable(claim, fmt.Sprintf("%s/dedi/lookup/%s/%s/%s?version_id=%s&proof=inclusion",
		strings.TrimRight(r.dediURL, "/"), pub.Namespace, pub.Registry,
		url.PathEscape(pub.Record), url.QueryEscape(pub.RegistryVersion)))
}

// authorityChain walks from the credential's issuerAuthority to the deployment.
func (h *handlers) authorityChain(ctx context.Context, issuerID string,
	cred schema.WorkEventCredential, def schema.Definition) chainWalk {
	var walk chainWalk
	auth := cred.CredentialSubject.IssuerAuthority
	if auth == nil {
		walk.NotEstablished = append(walk.NotEstablished,
			"who stands behind this credential — it names no issuer authority, so there is no chain from the credential to an organisation to walk")
		return walk
	}
	reader, ok := h.readerFor(issuerID)
	if !ok {
		walk.Links = append(walk.Links, asserted(
			fmt.Sprintf("issued on the authority of %s", auth.OrgID),
			fmt.Sprintf("the credential's own word; this deployment does not know where %s's registry can be read", issuerID)))
		walk.NotEstablished = append(walk.NotEstablished,
			fmt.Sprintf("that %s stood behind this credential — the issuer is trusted here by key only, and its registry is not configured, so the chain it names cannot be walked from this deployment", auth.OrgID))
		return walk
	}
	at := cred.ValidFrom

	var faces []authorizationFace
	walkOne := func(label string, ref *string, want authorizationWant) {
		if ref == nil {
			walk.NotEstablished = append(walk.NotEstablished,
				fmt.Sprintf("that %s held an %s when this work was attested — the credential names none", auth.OrgID, label))
			return
		}
		want.Ref = *ref
		pub, err := reader.publication(ctx, "authorization", *ref)
		switch {
		case errors.Is(err, errNotPublished):
			walk.Broken = append(walk.Broken, fmt.Sprintf(
				"the %s it names (%s) is not in the authorizations registry; it may still be pending publication, or it may never have been one an organisation held", label, *ref))
			return
		case err != nil:
			walk.Broken = append(walk.Broken, fmt.Sprintf("the %s it names (%s) could not be read: %v", label, *ref, err))
			return
		}
		var face authorizationFace
		if err := decodeFace(pub.Face, &face); err != nil {
			walk.Broken = append(walk.Broken, fmt.Sprintf("the %s it names (%s) is published in a shape this verifier cannot read: %v", label, *ref, err))
			return
		}
		if err := checkAuthorization(face, want); err != nil {
			walk.Broken = append(walk.Broken, "the "+label+" it names does not bear out its chain: "+err.Error())
			return
		}
		faces = append(faces, face)
		claim := fmt.Sprintf("%s held an %s when this was attested", auth.OrgID, label)
		if want.Scope == schema.AuthorizationScopeKindContext {
			claim = fmt.Sprintf("%s held a %s when this was attested", auth.OrgID, label)
			if face.Scope.ContextID != nil {
				claim += " for " + *face.Scope.ContextID
			}
		}
		claim += " (" + *ref + ")"
		walk.Links = append(walk.Links, reader.factLink(claim, pub))
		if since := revokedSince(face, at); since != nil {
			walk.NotEstablished = append(walk.NotEstablished, fmt.Sprintf(
				"that %s still holds that %s today — it was revoked on %s, after this credential was issued; the attestation stands, the standing behind it has changed",
				auth.OrgID, label, since.UTC().Format(time.RFC3339)))
		}
	}
	walkOne("instance-wide authorization", auth.QualificationRef,
		authorizationWant{OrgID: auth.OrgID, Scope: schema.AuthorizationScopeKindInstance, At: at})
	walkOne("context grant", auth.GrantRef,
		authorizationWant{OrgID: auth.OrgID, Scope: schema.AuthorizationScopeKindContext, ContextID: def.ContextID, At: at})
	if len(faces) > 0 && !attesterFunctionHeld(def, faces...) {
		walk.Broken = append(walk.Broken, fmt.Sprintf(
			"neither authorization it names carries a function %s@%d accepts for attesting (%s)",
			def.ID, def.Version, strings.Join(def.AuthorisedAttesterFunctions, ", ")))
	}

	// The organisation itself, in the organisations registry.
	orgClaim := fmt.Sprintf("%s is an organisation in the registry", auth.OrgID)
	switch pub, err := reader.publication(ctx, "organisation", auth.OrgID); {
	case errors.Is(err, errNotPublished):
		walk.Broken = append(walk.Broken, fmt.Sprintf(
			"the organisation it names (%s) is not in the organisations registry", auth.OrgID))
	case err != nil:
		walk.Broken = append(walk.Broken, fmt.Sprintf("the organisation it names (%s) could not be read: %v", auth.OrgID, err))
	default:
		var org struct {
			PartyID string `json:"partyId"`
			Kind    string `json:"kind"`
		}
		if err := decodeFace(pub.Face, &org); err != nil || org.PartyID != auth.OrgID || org.Kind != string(schema.PartyKindOrganisation) {
			walk.Broken = append(walk.Broken, fmt.Sprintf(
				"the registry's record of %s is not an organisation's", auth.OrgID))
			break
		}
		walk.Links = append(walk.Links, reader.factLink(orgClaim, pub))
	}

	// The deployment. Its published self-description names its operator and
	// the DID it issues under; both must be the ones this credential claims.
	var inst instanceView
	if err := reader.get(ctx, "/v1/instance", &inst); err != nil {
		walk.NotEstablished = append(walk.NotEstablished, fmt.Sprintf(
			"which deployment stands behind these registry facts — %s's self-description could not be read (%v)", reader.whose, err))
		return walk
	}
	switch {
	case inst.Instance.OperatorPartyID != auth.OrgID:
		walk.Broken = append(walk.Broken, fmt.Sprintf(
			"the deployment that holds these facts is operated by %s, not by the organisation the credential names (%s)",
			inst.Instance.OperatorPartyID, auth.OrgID))
	case inst.Instance.IssuerID != issuerID:
		walk.Broken = append(walk.Broken, fmt.Sprintf(
			"the deployment operated by %s issues as %s, not as this credential's issuer %s",
			auth.OrgID, inst.Instance.IssuerID, issuerID))
	default:
		claim := fmt.Sprintf("%s operates deployment %s, which issues as %s", auth.OrgID, inst.Instance.ID, issuerID)
		if inst.Publication == nil {
			walk.Links = append(walk.Links, asserted(claim, reader.whose+"'s own self-description, which it has not published to a registry"))
		} else {
			walk.Links = append(walk.Links, reader.factLink(claim, *inst.Publication))
		}
	}
	return walk
}

// decodeFace reads a published face into a typed view. Through JSON on
// purpose: the face is exactly the document on the log, and the typed view is
// a reading of it, not a second source.
func decodeFace(face map[string]any, out any) error {
	if face == nil {
		return errors.New("the publication carries no face")
	}
	raw, err := json.Marshal(face)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
