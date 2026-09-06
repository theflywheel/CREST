package verification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/pkg/strength"
)

func routes(mux *http.ServeMux, d service.Deps) {
	rawSeed, err := config.SecretStr("ISSUER_SEED", "")
	if err != nil {
		d.Log.Error("issuer seed unavailable", "error", err)
		panic(err)
	}
	issuerID := config.Str("ISSUER_ID", "")
	if err := validateIssuerDeployment(d.Config.Env, issuerID, rawSeed); err != nil {
		panic(err)
	}
	seed, err := credential.SeedFromBase64(rawSeed)
	if err != nil {
		d.Log.Error("issuer seed unusable", "error", err)
		panic(err)
	}
	issuer, err := credential.NewIssuer(issuerID, seed)
	if err != nil {
		d.Log.Error("issuer unusable", "error", err)
		panic(err)
	}
	issuerKeys, err := loadIssuerKeys(issuer)
	if err != nil {
		d.Log.Error("historical issuer keys unusable", "error", err)
		panic(err)
	}
	trustedIssuers, err := loadTrustedIssuers(issuer, issuerKeys)
	if err != nil {
		d.Log.Error("trusted issuer configuration unusable", "error", err)
		panic(err)
	}
	d.Log.Info("issuer ready", "issuer", issuer.ID(), "key", issuer.PublicKeyMultibase())

	h := &handlers{
		d:              d,
		definitions:    client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
		registry:       client.New(config.Str("PARTIES_URL", "http://parties:8080")),
		confirmation:   client.New(config.Str("CONFIRMATION_URL", "http://payments:8080")),
		evidence:       client.New(config.Str("EVIDENCE_URL", "http://evidence:8080")),
		dediURL:        config.Str("DEDI_URL", ""),
		issuer:         issuer,
		issuerKeys:     issuerKeys,
		trustedIssuers: trustedIssuers,
		statusListURL:  config.Str("STATUS_LIST_URL", "http://verification:8080/v1/status-list"),
	}
	mux.HandleFunc("POST /v1/verify", h.verify)
	mux.HandleFunc("POST /v1/verify/batch", h.verifyBatch)
	mux.HandleFunc("POST /v1/source-assessments", h.assess)
	mux.HandleFunc("GET /v1/source-assessments", h.assessments)
	mux.HandleFunc("DELETE /v1/source-assessments/{adapterRef}", h.clearAssessment)
	mux.HandleFunc("GET /v1/presentations", h.presentations)
	// §9's disclosure consent as a flow: request → per-share decision →
	// one collection of exactly the approved list (w1_15, w1_19, w1_20).
	registerShareRoutes(mux, d)
	// A verifier resolving a person rather than checking a document (#104).
	mux.HandleFunc("GET /v1/parties/{id}/credentials", h.partyCredentials)
	// Issuance and the credential record (#137): requested by the payments
	// application at window exit, owned here.
	mux.HandleFunc("POST /internal/credentials/issue", h.issue)
	// What Inji Certify's data-provider plugin reads at issuance (#155 phase
	// C): the confirmed-work facts for one provider subject, pairwise-derived
	// here so the salt never leaves this deployment. See certify.go.
	mux.HandleFunc("GET /internal/certify/work-events", h.certifyWorkEvents)
	mux.HandleFunc("GET /internal/credentials", h.listCredentialsRaw)
	mux.HandleFunc("GET /internal/credentials/{id}", h.getCredentialInternal)
	mux.HandleFunc("GET /internal/credentials/by-claim/{claimId}", h.credentialForClaim)
	mux.HandleFunc("POST /internal/credentials/{id}/revoke", h.revokeInternal)
	mux.HandleFunc("GET /v1/credentials", h.listCredentials)
	mux.HandleFunc("GET /v1/credentials/{id}", h.getCredentialByID)
	// The printed card (#24, §5): the holding mechanism for a worker with no
	// phone. HTML by default because it is meant to reach a printer;
	// ?format=payload gives the bare QR string for a print station with its
	// own stationery.
	mux.HandleFunc("GET /v1/credentials/{id}/card", h.credentialCard)
	mux.HandleFunc("POST /v1/credentials/{id}/revoke", h.revoke)
	mux.HandleFunc("POST /v1/credentials/{id}/custody-transfer", h.transferCustody)
	mux.HandleFunc("GET /v1/status-list", h.statusList)
	mux.HandleFunc("GET /v1/issuer", h.issuerInfo)
}

type handlers struct {
	d service.Deps

	// dediURL is where this deployment's registry node can be reached, so a
	// verdict can hand the verifier a URL instead of a promise. Empty when the
	// deployment runs on the Postgres fallback, and the trust chain says so.
	dediURL string

	definitions  *client.Client
	registry     *client.Client
	confirmation *client.Client
	evidence     *client.Client

	issuer         *credential.Issuer
	issuerKeys     map[string]string
	trustedIssuers map[string]trustedIssuer
	statusListURL  string
}

// trustedIssuer is deployment configuration, never data taken from a
// credential. Keys and resolver URLs must be registered by the operator before
// an external issuer can affect a verdict.
type trustedIssuer struct {
	Keys           map[string]string `json:"keys"`
	Definitions    string            `json:"definitions"`
	DefinitionURLs []string          `json:"definitionURLs"`
	StatusList     string            `json:"statusList"`
	StatusListURLs []string          `json:"statusListURLs"`
}

func loadIssuerKeys(current *credential.Issuer) (map[string]string, error) {
	keys := map[string]string{current.VerificationMethod(): current.PublicKeyMultibase()}
	raw := config.Str("ISSUER_HISTORICAL_KEYS_JSON", "")
	if raw == "" {
		return keys, nil
	}
	var historical map[string]string
	if err := json.Unmarshal([]byte(raw), &historical); err != nil {
		return nil, fmt.Errorf("ISSUER_HISTORICAL_KEYS_JSON: %w", err)
	}
	for method, key := range historical {
		if strings.TrimSpace(method) != "" && strings.TrimSpace(key) != "" {
			keys[method] = key
		}
	}
	return keys, nil
}

func loadTrustedIssuers(current *credential.Issuer, currentKeys map[string]string) (map[string]trustedIssuer, error) {
	out := map[string]trustedIssuer{current.ID(): {Keys: currentKeys}}
	raw := config.Str("TRUSTED_ISSUERS_JSON", "")
	if raw == "" {
		return out, nil
	}
	var configured map[string]trustedIssuer
	if err := json.Unmarshal([]byte(raw), &configured); err != nil {
		return nil, fmt.Errorf("TRUSTED_ISSUERS_JSON: %w", err)
	}
	for issuerID, item := range configured {
		if strings.TrimSpace(issuerID) == "" || len(item.Keys) == 0 {
			return nil, fmt.Errorf("trusted issuer %q must name at least one public key", issuerID)
		}
		for method, key := range item.Keys {
			if !strings.HasPrefix(method, issuerID+"#") || strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("trusted issuer %q has an invalid verification method", issuerID)
			}
		}
		for name, value := range map[string]string{"definitions": item.Definitions, "statusList": item.StatusList} {
			if value != "" && !trustedResolverURL(value) {
				return nil, fmt.Errorf("trusted issuer %q has an unsafe %s resolver URL", issuerID, name)
			}
		}
		for _, value := range append(append([]string{}, item.DefinitionURLs...), item.StatusListURLs...) {
			if !trustedResolverURL(value) {
				return nil, fmt.Errorf("trusted issuer %q has an unsafe historical resolver URL", issuerID)
			}
		}
		if issuerID == current.ID() {
			merged := trustedIssuer{
				Keys: currentKeys, Definitions: item.Definitions, DefinitionURLs: item.DefinitionURLs,
				StatusList: item.StatusList, StatusListURLs: item.StatusListURLs,
			}
			for method, key := range item.Keys {
				merged.Keys[method] = key
			}
			out[issuerID] = merged
		} else {
			out[issuerID] = item
		}
	}
	return out, nil
}

func trustedResolverURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// Verdict is what a verifier is handed.
//
// The tier is here and is not stored anywhere — it is computed on this request,
// against the definition version the credential pinned and the assessment of
// its source as of now (§6). Ask again tomorrow and the answer may differ; that
// is the feature, not a caveat.
type Verdict struct {
	Valid   bool     `json:"valid"`
	Reasons []string `json:"reasons"`

	Tier       *int     `json:"tier,omitempty"`
	TierReason []string `json:"tierReason,omitempty"`

	// TrustChain is what the verifier can walk for themselves. A verdict
	// nobody can check is an assertion, and this service asserting things is
	// exactly what §7 is trying to avoid.
	//
	// Each link says whether it is checkable and where, because "trust chain"
	// as a list of sentences quietly conflates two different things: facts the
	// verifier can confirm without CREST, and facts CREST is telling them. A
	// verifier who cannot tell those apart is trusting the whole list.
	TrustChain []Link `json:"trustChain,omitempty"`

	// NotEstablished is what a *valid* verdict does not prove.
	//
	// This exists because of finding #68. A green verdict invites the reading
	// "and therefore this person was authorised to do this work", and that is
	// not something this deployment can demonstrate to a stranger — a worker's
	// authorization is deliberately not published, because on an append-only
	// log it is a permanent public roster of who works where. Saying so in the
	// verdict is the difference between a limit that is disclosed and one the
	// verifier discovers by assuming wrongly.
	NotEstablished []string `json:"notEstablished,omitempty"`

	// Contested is every dispute standing against this credential or the claim
	// behind it (#58).
	//
	// Orthogonal to Valid, and that separation is the decision. `valid` means
	// this issuer really asserted this and has not withdrawn it — a question
	// about the document. `contested` means somebody says the underlying record
	// is wrong — a question about the world. Collapsing them would mean a
	// worker who disputes one detail loses the whole credential for work they
	// did do, which is a penalty for objecting, falling on the person the
	// dispute exists to protect.
	//
	// Empty when nothing is disputed. Never populated with the reason or with
	// who raised it: that is ordinarily the worker speaking about their own
	// record, and it is between them and the programme.
	Contested []ContestStanding `json:"contested,omitempty"`

	SignatureValid bool `json:"signatureValid"`
	Revoked        bool `json:"revoked"`
	// The online status check time is explicit so a consumer never mistakes a
	// cached verdict for an offline freshness guarantee.
	StatusCheckedAt time.Time `json:"statusCheckedAt,omitempty"`
}

// ContestStanding is where one dispute stands. Standing only — see Verdict.
type ContestStanding struct {
	State    string    `json:"state"`
	RaisedAt time.Time `json:"raisedAt"`
	// Against says whether the dispute names this credential or the claim it
	// projects. A verifier reading "UPHELD" should be able to tell whether
	// somebody is contesting the document or the work.
	Against string `json:"against"`
}

// Link is one step of the trust chain.
type Link struct {
	Claim string `json:"claim"`

	// Checkable reports whether the verifier can confirm this link without
	// taking CREST's word for it.
	Checkable bool `json:"checkable"`

	// How is where they go to check it — a URL they can fetch, or the name of
	// the artefact they already hold. Set only when Checkable.
	How string `json:"how,omitempty"`

	// Trusting names what they are relying on instead, when they cannot.
	// Never empty when Checkable is false: "you must trust something" without
	// saying what is not a disclosure.
	Trusting string `json:"trusting,omitempty"`
}

// checkable and asserted are the only two ways to build a Link, so a link
// cannot be constructed without answering the question.
func checkable(claim, how string) Link {
	return Link{Claim: claim, Checkable: true, How: how}
}

func asserted(claim, trusting string) Link {
	return Link{Claim: claim, Checkable: false, Trusting: trusting}
}

func (h *handlers) verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential map[string]any `json:"credential"`
		// What the presentation is for. Showing the bare payload is itself
		// consent for the bare payload; a scoped request has to say who is
		// asking and why, and that is recorded (§9).
		RequestedByPartyID string `json:"requestedByPartyId"`
		Purpose            string `json:"purpose"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.Credential == nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "no credential to verify")
		return
	}

	verdict, subjectRef, credID := h.assess1(r.Context(), req.Credential)

	scope := "bare"
	if req.RequestedByPartyID != "" || req.Purpose != "" {
		scope = "scoped"
		if req.RequestedByPartyID == "" || !authorizeParty(w, r, h.d, req.RequestedByPartyID) {
			return
		}
	}
	if err := h.record(r.Context(), presentation{
		ID: id.New(h.d.Clock, "presentation"), CredentialID: credID,
		SubjectRef: subjectRef, RequestedBy: req.RequestedByPartyID, Purpose: req.Purpose,
		Scope: scope, Outcome: outcomeOf(verdict), Tier: verdict.Tier,
		CreatedAt: h.d.Clock.Now(),
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record presentation", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, verdict)
}

// assess1 is the verification itself, in the order a sceptic would do it:
// is the signature real, has it been withdrawn, and only then how strong is it.
//
// Order matters. Computing a tier for a forged credential and reporting both
// would put a number next to something that should only ever read "not valid".
func (h *handlers) assess1(ctx context.Context, doc map[string]any) (Verdict, string, string) {
	v := Verdict{}
	credID, _ := doc["id"].(string)

	subjectRef := ""
	if subject, ok := doc["credentialSubject"].(map[string]any); ok {
		subjectRef, _ = subject["id"].(string)
	}

	issuerKey, issuerID, err := h.issuerKey(ctx, doc)
	if err != nil {
		v.Reasons = append(v.Reasons, "the issuer's key could not be resolved: "+err.Error())
		return v, subjectRef, credID
	}
	if err := credential.Verify(doc, issuerKey); err != nil {
		v.Reasons = append(v.Reasons, "signature: "+err.Error())
		return v, subjectRef, credID
	}
	v.SignatureValid = true
	// Checkable: the proof is on the credential and the key resolves from the
	// issuer's own did:web document. This deployment is not in the loop.
	v.TrustChain = append(v.TrustChain,
		checkable("signed by "+issuerID, issuerID+" resolves to a DID document carrying the key; the proof is on the credential itself"))

	revoked, err := h.revoked(ctx, doc)
	if err != nil {
		v.Reasons = append(v.Reasons, "the status list could not be checked: "+err.Error())
		return v, subjectRef, credID
	}
	v.Revoked = revoked
	v.StatusCheckedAt = h.d.Clock.Now()
	if revoked {
		v.Reasons = append(v.Reasons, "this credential has been withdrawn")
		return v, subjectRef, credID
	}
	// Checkable: the status list is itself a signed credential, and a bitstring
	// is designed to be fetched whole so that reading one bit does not tell the
	// issuer which credential is being checked (§9).
	v.TrustChain = append(v.TrustChain,
		checkable("not withdrawn on the deployment's status list",
			statusListURL(doc)))

	cred, err := parse(doc)
	if err != nil {
		v.Reasons = append(v.Reasons, "the credential is not a WorkEventCredential: "+err.Error())
		return v, subjectRef, credID
	}

	def, err := h.definition(ctx, cred.CredentialSubject.WorkEvent.Definition, issuerID)
	if err != nil {
		v.Reasons = append(v.Reasons, "the definition it names could not be resolved: "+err.Error())
		return v, subjectRef, credID
	}
	// The definition version is where #69 changed the answer. It used to be
	// readable only from this deployment's database, which made it an
	// assertion; now an ACTIVE definition is published to the registry
	// substrate, and where that substrate is a transparency log the verifier
	// can resolve the exact version this credential pinned and check its
	// inclusion proof. Where it is not — the Postgres fallback — the link says
	// so rather than quietly reading the same either way.
	v.TrustChain = append(v.TrustChain, h.definitionLink(ctx, def, cred.CredentialSubject.WorkEvent.DefinitionProof))

	if !issuerAuthorised(def, issuerID) {
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"%s is not an authorised issuer for %s@%d", issuerID, def.ID, def.Version))
		return v, subjectRef, credID
	}
	// Follows from the definition, so it is checkable exactly when the
	// definition is: the authorised-issuer list is part of the verifier face
	// that gets published.
	v.TrustChain = append(v.TrustChain, issuerLink(v.TrustChain, issuerID, def))

	assessment, err := h.assessmentFor(ctx, h.credentialContext(ctx, credID),
		provenanceSystemRef(cred.CredentialSubject.Provenance), cred.CredentialSubject.Provenance.AdapterRef)
	if err != nil {
		v.Reasons = append(v.Reasons, "the source assessment could not be read: "+err.Error())
		return v, subjectRef, credID
	}

	// Identity assurance is asked for now rather than read off the credential,
	// for the same reason as everything else here: a worker who anchored their
	// identity last week must see that reflected in a credential issued last
	// year (§4.1).
	assurance := h.assurance(ctx, subjectRef)

	result := strength.Evaluate(strength.Facts{
		Provenance:        cred.CredentialSubject.Provenance,
		PresentFields:     presentFields(cred),
		IdentityAssurance: assurance,
	}, def, assessment)

	v.TierReason = result.Because
	if !result.Acceptable {
		v.Reasons = append(v.Reasons,
			"the evidence behind this credential no longer meets the definition it was issued under")
		return v, subjectRef, credID
	}
	tier := result.Tier
	v.Tier = &tier
	v.Valid = true

	// Resolved from the claim the credential names — which it can, because a
	// credential carries its claimId (#16). Reported beside the verdict rather
	// than folded into it: the credential is a historical statement and stays
	// valid, and the dispute is a separate fact about the same work.
	v.Contested = h.contests(ctx, credID, cred.CredentialSubject.WorkEvent.ClaimID)

	// Stated on a valid verdict, because that is the one a verifier acts on.
	// Finding #68: a worker's authorization is not published, so whether this
	// particular person was authorised for this project is not something a
	// stranger can confirm — and a green verdict reads as though it were.
	v.NotEstablished = append(v.NotEstablished,
		"that the subject was authorised to do this work for this programme — "+
			"a worker's authorization is deliberately not published, because on an "+
			"append-only log it would be a permanent public record of who works where (#68). "+
			"This deployment holds that authorization and can attest to it; you cannot check it independently.")
	if assurance == schema.IdentityAssuranceIA0 {
		// Distinct from the above and worth its own sentence: the credential
		// verifies, and nothing here ties it to a person whose identity was
		// ever checked.
		v.NotEstablished = append(v.NotEstablished,
			"that the subject is who they say they are — this deployment holds no identity binding "+
				"for the credential's subject, so the tier above was computed at the weakest assurance")
	}
	return v, subjectRef, credID
}

// definitionLink resolves where, if anywhere, the verifier can fetch the
// definition version this credential pinned.
func (h *handlers) definitionLink(ctx context.Context, def schema.Definition,
	pin *schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof) Link {
	claim := fmt.Sprintf("measured under %s@%d, %s", def.ID, def.Version, def.Activity.Label)

	// The pin the credential itself carries is preferred over anything this
	// deployment can look up (#16).
	//
	// The difference is the whole point. A pin read from our own definitions
	// service is this deployment's current opinion about where that version
	// lives; a pin inside the signature is what the issuer committed to at the
	// moment they signed, and it cannot be revised afterwards by anyone —
	// including us. A verifier following the second is checking the issuer's
	// claim; a verifier following the first is asking the issuer to confirm
	// themselves.
	if pin != nil {
		// The digest goes in the claim, not the URL. `how` is fetched — by a
		// verifier, and by the e2e scenario that asserts it answers — so
		// anything appended to it stops being an address.
		claim += fmt.Sprintf(", pinned to registry record %s@%s with digest %s",
			pin.Record, pin.Version, pin.Digest)
		if h.dediURL == "" {
			// We know exactly what to resolve and not where the log is. The
			// deployment's published self-description carries that (#70) and
			// nothing reads it yet — recorded honestly rather than guessed.
			return asserted(claim,
				"the credential names the registry record to check, and this deployment does not publish where its registry node can be reached")
		}
		return checkable(claim, fmt.Sprintf("%s/dedi/lookup/%s/%s/%s?version_id=%s&proof=inclusion",
			strings.TrimRight(h.dediURL, "/"), pin.Namespace, pin.Registry,
			url.PathEscape(pin.Record), url.QueryEscape(pin.Version)))
	}

	var pub struct {
		Namespace       string `json:"namespace"`
		Registry        string `json:"registry"`
		Record          string `json:"record"`
		RegistryVersion string `json:"registryVersion"`
		Transparent     bool   `json:"transparent"`
	}
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s/publication?version=%d",
		url.PathEscape(def.ID), def.Version), &pub)
	switch {
	case err != nil:
		// Not yet published, or the publication could not be read. Either way
		// the verifier is reading this deployment's copy.
		return asserted(claim, "this deployment's own record of the definition; it is not resolvable elsewhere")
	case !pub.Transparent:
		// The Postgres fallback. Published, in the sense that a record exists —
		// but with no inclusion proof there is nothing for a verifier to check
		// that is not just this deployment answering again.
		return asserted(claim,
			"this deployment's registry, which is running without a transparency log, so the published copy proves nothing a verifier can check")
	case h.dediURL == "":
		return asserted(claim, "this deployment's own record; it does not publish where its registry node can be reached")
	}
	return checkable(claim, fmt.Sprintf("%s/dedi/lookup/%s/%s/%s?version_id=%s&proof=inclusion",
		strings.TrimRight(h.dediURL, "/"), pub.Namespace, pub.Registry,
		url.PathEscape(pub.Record), url.QueryEscape(pub.RegistryVersion)))
}

// issuerLink inherits the definition link's checkability, because the
// authorised-issuer list lives inside the definition. Claiming this is
// independently checkable when the document it came from is not would be
// exactly the conflation the Link type exists to prevent.
func issuerLink(chain []Link, issuerID string, def schema.Definition) Link {
	claim := fmt.Sprintf("%s is named an authorised issuer by %s@%d", issuerID, def.ID, def.Version)
	if len(chain) > 0 {
		if prev := chain[len(chain)-1]; prev.Checkable {
			return checkable(claim, "the verifier face of the definition record above")
		}
	}
	return asserted(claim, "this deployment's copy of the definition")
}

// statusListURL is where the verifier fetches the list for themselves. The
// credential carries it, so this is the credential's own answer rather than
// this service's.
func statusListURL(doc map[string]any) string {
	status, ok := doc["credentialStatus"].(map[string]any)
	if !ok {
		return "the status list named by the credential"
	}
	if u, ok := status["statusListCredential"].(string); ok && u != "" {
		return u
	}
	return "the status list named by the credential"
}

// partyCredentials is what a verifier resolving a party sees: every credential
// in the person's chain, and nothing about the chain itself (#104, §16).
//
// The ruling this implements: continuity wins over hiding history, and the
// merge itself is not disclosed. A worker whose duplicate record was closed
// proves their whole history here — asking about either id returns the same
// credentials — but the response carries no identifier list, no merged flag,
// and no count of underlying records, because "this worker was recorded twice"
// is a fact about them they never volunteered. What the credentials' own
// subject fields reveal is a property of the credentials, stated in §16 rather
// than papered over here.
//
// Each credential returned is recorded in the presentation trail, one entry
// per credential — a worker asking "who checked me" must see this kind of
// check like any other (§9).
func (h *handlers) partyCredentials(w http.ResponseWriter, r *http.Request) {
	partyID := r.PathValue("id")
	if !authorizeParty(w, r, h.d, partyID) {
		return
	}
	// The credential record is local since #137, but the merge expansion is
	// still the registry's answer. Refused rather than emptied when it cannot
	// be read: an empty list reads as "this person has no history", which is a
	// different and false statement.
	ids, err := h.d.SameParty(r.Context(), partyID)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "registry_unavailable",
			"the registry could not say which ids are this party, so this answer would be incomplete without saying so")
		return
	}
	if len(ids) == 0 {
		ids = []string{partyID}
	}
	var out struct {
		Credentials []json.RawMessage `json:"credentials"`
	}
	out.Credentials, err = credentialsFor(r.Context(), h.d.DB.Q(), ids)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list credentials", err)
		return
	}
	requestedBy := r.URL.Query().Get("requestedByPartyId")
	caller := identity.From(r.Context())
	if requestedBy != "" && requestedBy != caller.PartyID {
		httpx.WriteError(w, http.StatusForbidden, "requester_mismatch",
			"requestedByPartyId must identify the verified caller")
		return
	}
	if requestedBy == "" {
		requestedBy = caller.PartyID
	}
	purpose := r.URL.Query().Get("purpose")
	scope := "bare"
	if requestedBy != "" || purpose != "" {
		scope = "scoped"
	}
	now := h.d.Clock.Now()
	for _, doc := range out.Credentials {
		var cred struct {
			ID      string `json:"id"`
			Subject struct {
				ID string `json:"id"`
			} `json:"credentialSubject"`
		}
		_ = json.Unmarshal(doc, &cred)
		if err := h.record(r.Context(), presentation{
			ID: id.New(h.d.Clock, "presentation"), CredentialID: cred.ID,
			SubjectRef: cred.Subject.ID, RequestedBy: requestedBy, Purpose: purpose,
			Scope: scope, Outcome: "listed", CreatedAt: now,
		}); err != nil {
			httpx.Fail(w, h.d.Log, "record presentation", err)
			return
		}
	}
	if out.Credentials == nil {
		out.Credentials = []json.RawMessage{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"credentials": out.Credentials, "count": len(out.Credentials),
	})
}

// contests asks confirmation where any dispute against this credential stands.
//
// Best-effort, and the failure mode is stated because it is the wrong way
// round: an unreachable confirmation service produces a verdict that reports no
// dispute, which is indistinguishable from there being none. That is logged
// loudly. It is accepted here because the alternative — refusing to verify a
// credential because a service that holds *disputes* is down — would make every
// verification depend on a component that has nothing to do with whether the
// signature is real.
func (h *handlers) contests(ctx context.Context, credentialID, claimID string) []ContestStanding {
	var out []ContestStanding
	for _, t := range []struct{ kind, id string }{
		{"credential", credentialID},
		{"claim", claimID},
	} {
		if t.id == "" {
			continue
		}
		var resp struct {
			Contests []ContestStanding `json:"contests"`
		}
		if err := h.confirmation.Get(ctx, fmt.Sprintf("/internal/contests?targetKind=%s&targetId=%s",
			t.kind, url.QueryEscape(t.id)), &resp); err != nil {
			h.d.Log.Warn("could not read disputes; this verdict cannot say whether the record is contested",
				"target", t.kind, "id", t.id, "error", err)
			continue
		}
		for _, c := range resp.Contests {
			c.Against = t.kind
			out = append(out, c)
		}
	}
	return out
}

// issuerKey resolves the verification key.
//
// The issuer is local since #137. A field verifier does it differently — they
// hold the key already, which is what makes W6's offline verification
// possible; pkg/credential.Verify takes the key as an argument precisely so
// that path exists.
func (h *handlers) issuerKey(_ context.Context, doc map[string]any) (string, string, error) {
	issuerID, _ := doc["issuer"].(string)
	if issuerID == "" {
		return "", issuerID, errors.New("the credential carries no issuer")
	}
	proof, _ := doc["proof"].(map[string]any)
	method, _ := proof["verificationMethod"].(string)
	if method == "" {
		method = issuerID + "#key-1"
	}
	if !strings.HasPrefix(method, issuerID+"#") {
		return "", issuerID, fmt.Errorf("verification method %s is not under issuer %s", method, issuerID)
	}
	if trusted, ok := h.trustedIssuers[issuerID]; ok {
		if key, found := trusted.Keys[method]; found {
			return key, issuerID, nil
		}
		return "", issuerID, fmt.Errorf("no trusted key for verification method %s", method)
	}
	if key, ok := h.issuerKeys[method]; ok {
		return key, issuerID, nil
	}
	if method == h.issuer.VerificationMethod() && issuerID == h.issuer.ID() {
		return h.issuer.PublicKeyMultibase(), issuerID, nil
	}
	if issuerID != "" && h.issuer.ID() != issuerID {
		return "", issuerID, fmt.Errorf("this deployment does not hold a key for %s", issuerID)
	}
	return "", issuerID, fmt.Errorf("no trusted key for verification method %s", method)
}

// revoked fetches the whole status list and reads one bit of it.
//
// The whole list, on purpose: asking about one credential tells the issuer
// which credential is being checked, and the point of a bitstring is that it
// does not (§9).
func (h *handlers) revoked(ctx context.Context, doc map[string]any) (bool, error) {
	status, ok := doc["credentialStatus"].(map[string]any)
	if !ok {
		return false, errors.New("the credential carries no status entry")
	}
	indexText, _ := status["statusListIndex"].(string)
	var index int
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
		return false, fmt.Errorf("status index %q is not a number", indexText)
	}

	issuerID, _ := doc["issuer"].(string)
	statusURL, _ := status["statusListCredential"].(string)
	if issuerID == h.issuer.ID() {
		if statusURL != h.statusListURL {
			return false, fmt.Errorf("credential names an untrusted status list %s", statusURL)
		}
	}
	if trusted, ok := h.trustedIssuers[issuerID]; ok && issuerID != h.issuer.ID() {
		statusEndpoint := trustedStatusEndpoint(trusted, statusURL)
		if statusEndpoint == "" {
			return false, fmt.Errorf("credential names a status list that is not registered for issuer %s", issuerID)
		}
		statusDoc, err := fetchJSON(ctx, statusEndpoint)
		if err != nil {
			return false, fmt.Errorf("fetch trusted status list: %w", err)
		}
		key, _, err := h.issuerKey(ctx, statusDoc)
		if err != nil {
			return false, err
		}
		if err := credential.Verify(statusDoc, key); err != nil {
			return false, fmt.Errorf("trusted status list signature: %w", err)
		}
		if statusDoc["issuer"] != issuerID || statusDoc["id"] != statusEndpoint {
			return false, errors.New("trusted status list identity does not match its configured issuer")
		}
		if !statusFresh(statusDoc, h.d.Clock.Now()) {
			return false, errors.New("trusted status list is outside its signed validity window")
		}
		subject, _ := statusDoc["credentialSubject"].(map[string]any)
		encoded, _ := subject["encodedList"].(string)
		list, err := credential.DecodeStatusList(encoded)
		if err != nil {
			return false, err
		}
		return list.Revoked(index), nil
	}

	// The list is local since #137 — this service is the issuer, and the row
	// it reads is the row it signs for everyone else. A remote verifier still
	// fetches the signed document from /v1/status-list and checks the
	// signature, because for them, whoever answers the URL would otherwise
	// decide who has been revoked.
	list, err := loadStatusList(ctx, h.d.DB.Q())
	if err != nil {
		return false, err
	}
	return list.Revoked(index), nil
}

func (h *handlers) definition(ctx context.Context, ref schema.VersionedRef, issuerID string) (schema.Definition, error) {
	if trusted, ok := h.trustedIssuers[issuerID]; ok && issuerID != h.issuer.ID() {
		bases := append([]string{}, trusted.DefinitionURLs...)
		if trusted.Definitions != "" {
			bases = append([]string{trusted.Definitions}, bases...)
		}
		var lastErr error
		for _, base := range bases {
			var def schema.Definition
			path := strings.TrimRight(base, "/") + "/" + url.PathEscape(ref.ID) + "?version=" + url.QueryEscape(fmt.Sprint(ref.Version))
			if err := fetchJSONInto(ctx, path, &def); err == nil {
				if def.ID != ref.ID || def.Version != ref.Version {
					lastErr = fmt.Errorf("trusted definition resolver returned %s@%d for requested %s@%d",
						def.ID, def.Version, ref.ID, ref.Version)
					continue
				}
				return def, nil
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return schema.Definition{}, fmt.Errorf("fetch trusted definition: %w", lastErr)
		}
		return schema.Definition{}, errors.New("no trusted definition resolver configured")
	}
	var def schema.Definition
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s?version=%d",
		url.PathEscape(ref.ID), ref.Version), &def)
	return def, err
}

func trustedStatusEndpoint(trusted trustedIssuer, statusURL string) string {
	if trusted.StatusList != "" && trusted.StatusList == statusURL {
		return trusted.StatusList
	}
	for _, candidate := range trusted.StatusListURLs {
		if candidate == statusURL {
			return candidate
		}
	}
	return ""
}

func fetchJSON(ctx context.Context, endpoint string) (map[string]any, error) {
	var out map[string]any
	err := fetchJSONInto(ctx, endpoint, &out)
	return out, err
}

func fetchJSONInto(ctx context.Context, endpoint string, out any) error {
	if !safeFetchURL(endpoint) {
		return errors.New("resolver URL is not an absolute HTTP(S) URL")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("resolver redirects are refused")
	}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("resolver answered HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

func safeFetchURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == ""
}

func statusFresh(doc map[string]any, now time.Time) bool {
	from, okFrom := parseTimestamp(doc["validFrom"])
	until, okUntil := parseTimestamp(doc["validUntil"])
	return okFrom && okUntil && !now.Before(from) && now.Before(until)
}

func parseTimestamp(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

func (h *handlers) assurance(ctx context.Context, partyID string) schema.IdentityAssurance {
	var out struct {
		IdentityAssurance schema.IdentityAssurance `json:"identityAssurance"`
	}
	if err := h.registry.Get(ctx,
		"/internal/parties/"+url.PathEscape(partyID)+"/assurance", &out); err != nil {
		// A subject this deployment cannot resolve is IA-0, not an error: a
		// credential from elsewhere is still verifiable, it is simply the
		// weakest identity claim available.
		return schema.IdentityAssuranceIA0
	}
	return out.IdentityAssurance
}

func issuerAuthorised(def schema.Definition, issuerID string) bool {
	for _, allowed := range def.Faces.Verifier.AuthorisedIssuers {
		if allowed == issuerID {
			return true
		}
	}
	return false
}

// presentFields is which of the tier map's required fields the credential
// actually carries. The credential carries them because a verifier cannot ask
// CREST — offline is the case that matters (W6).
// presentFields is which fields the source record carried, as the credential
// records them. Read from the credential rather than from CREST, because that
// is what an offline verifier has — and if this service used a richer source
// than they do, it would report a tier they cannot reproduce.
func presentFields(cred schema.WorkEventCredential) []string {
	return cred.CredentialSubject.WorkEvent.EvidenceFields
}

func parse(doc map[string]any) (schema.WorkEventCredential, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return schema.WorkEventCredential{}, err
	}
	var cred schema.WorkEventCredential
	if err := json.Unmarshal(raw, &cred); err != nil {
		return schema.WorkEventCredential{}, err
	}
	return cred, nil
}

// assess records how this deployment regards a source. One call downgrades
// every credential that source produced, immediately and with no reissuance.
func (h *handlers) assess(w http.ResponseWriter, r *http.Request) {
	// Re-grading a source moves every affected credential's tier — a signed-in operations surface (#102).
	var req struct {
		AdapterRef string `json:"adapterRef"`
		ContextID  string `json:"contextId"`
		SystemRef  string `json:"systemRef"`
		MaxTier    int    `json:"maxTier"`
		Reason     string `json:"reason"`
		AssessedBy string `json:"assessedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.AdapterRef == "" || req.ContextID == "" || req.SystemRef == "" || req.Reason == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"downgrading a source needs its registered adapter, context, system reference and reason")
		return
	}
	if req.MaxTier < 1 || req.MaxTier > 3 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "maxTier is 1 to 3 (reference numbering)")
		return
	}
	assessor, ok := h.authorizeSourceAssessment(w, r, req.AdapterRef, req.ContextID, req.SystemRef)
	if !ok {
		return
	}
	// Never persist the party name supplied in JSON as the actor. The body is
	// only a source selector; the authenticated principal is the audit actor.
	req.AssessedBy = assessor
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO source_assessments (adapter_ref, context_id, system_ref, max_tier, reason, assessed_by, assessed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (context_id, system_ref) WHERE system_ref IS NOT NULL AND context_id IS NOT NULL DO UPDATE
			SET max_tier = EXCLUDED.max_tier, reason = EXCLUDED.reason,
			    assessed_by = EXCLUDED.assessed_by, assessed_at = EXCLUDED.assessed_at,
			    adapter_ref = EXCLUDED.adapter_ref`,
			req.AdapterRef, req.ContextID, req.SystemRef, req.MaxTier, req.Reason, req.AssessedBy, h.d.Clock.Now())
		return err
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record source assessment", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearAssessment lifts a downgrade.
//
// As real an operation as imposing one: a source that was under investigation
// and has been cleared must be able to return to full strength, and every
// credential it produced recovers with it — again with no reissuance, because
// the tier was never stored. A system that can only ever downgrade a source
// ratchets one way and eventually trusts nothing.
func (h *handlers) clearAssessment(w http.ResponseWriter, r *http.Request) {
	// Lifting a downgrade restores trust — a signed-in operations surface (#102).
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	systemRef := strings.TrimSpace(r.URL.Query().Get("systemRef"))
	if _, ok := h.authorizeSourceAssessment(w, r, r.PathValue("adapterRef"), contextID, systemRef); !ok {
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		_, err := tx.Exec(r.Context(),
			`DELETE FROM source_assessments WHERE system_ref = $1
			   AND (context_id = $2 OR context_id IS NULL)`, systemRef, contextID)
		return err
	}); err != nil {
		httpx.Fail(w, h.d.Log, "clear source assessment", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// assessmentFor answers with the project's own assessment of a source. A row
// assessed before assessments were scoped (context_id NULL) still applies to
// every project until it is re-assessed or cleared in one; a scoped row wins.
func (h *handlers) assessmentFor(ctx context.Context, contextID, systemRef, adapterRef string) (*strength.SourceAssessment, error) {
	var maxTier int
	var reason string
	var err error
	if systemRef != "" {
		err = h.d.DB.Q().QueryRow(ctx,
			`SELECT max_tier, reason FROM source_assessments
			  WHERE system_ref = $1 AND (context_id = $2 OR context_id IS NULL)
			  ORDER BY context_id NULLS LAST LIMIT 1`, systemRef, contextID).
			Scan(&maxTier, &reason)
	} else {
		// A credential without systemRef predates the scoped provenance field.
		// Only those historical credentials may consult the legacy adapter key;
		// a current credential must never fall back to a shared adapter class.
		err = h.d.DB.Q().QueryRow(ctx,
			`SELECT max_tier, reason FROM source_assessments WHERE system_ref IS NULL AND adapter_ref = $1`, adapterRef).
			Scan(&maxTier, &reason)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil // no concerns recorded
	}
	if err != nil {
		return nil, err
	}
	return &strength.SourceAssessment{MaxTier: maxTier, Reason: reason}, nil
}

// provenanceSystemRef reads the optional field by JSON name so this service
// remains compatible with credentials issued before the generated schema
// gained the scoped systemRef field. An absent field is the only case allowed
// to use the historical adapter assessment fallback.
// credentialContext is the project a credential this deployment issued was
// issued in; "" for a credential issued elsewhere, which then meets only the
// unscoped assessments.
func (h *handlers) credentialContext(ctx context.Context, credID string) string {
	var contextID *string
	if err := h.d.DB.Q().QueryRow(ctx,
		`SELECT context_id FROM credentials WHERE id = $1`, credID).Scan(&contextID); err != nil || contextID == nil {
		return ""
	}
	return *contextID
}

func provenanceSystemRef(provenance schema.Provenance) string {
	raw, err := json.Marshal(provenance)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	value, _ := fields["systemRef"].(string)
	return strings.TrimSpace(value)
}

type registeredSourceView struct {
	AdapterRef   string `json:"adapterRef"`
	ContextID    string `json:"contextId"`
	SystemRef    string `json:"systemRef"`
	OwnerPartyID string `json:"ownerPartyId"`
}

// authorizeSourceAssessment binds the mutation to a source registration and
// to the actual authenticated source owner/operator. AdapterRef alone is an
// adapter class shared by many feeds and is never sufficient authority.
func (h *handlers) authorizeSourceAssessment(w http.ResponseWriter, r *http.Request,
	adapterRef, contextID, systemRef string) (string, bool) {
	if !identity.Authenticated(w, r, h.d.Log, true) {
		return "", false
	}
	caller := identity.From(r.Context())
	if caller.PartyID == "" || caller.Assisting() {
		httpx.WriteError(w, http.StatusForbidden, "direct_source_operator_required",
			"source assessments must be performed directly by an enrolled source owner or operator")
		return "", false
	}
	var source registeredSourceView
	path := "/internal/sources/by-system/" + url.PathEscape(systemRef) + "?contextId=" + url.QueryEscape(contextID)
	if err := h.evidence.Get(r.Context(), path, &source); err != nil {
		httpx.WriteError(w, http.StatusForbidden, "source_not_registered",
			"the source assessment must name a registered source in this project")
		return "", false
	}
	if source.AdapterRef != adapterRef || source.ContextID != contextID || source.SystemRef != systemRef {
		httpx.WriteError(w, http.StatusConflict, "source_identity_mismatch",
			"the adapter, context and system reference do not match the registered source")
		return "", false
	}
	// The registered source owner may act directly. A different operator must
	// hold the source-owner function in this project; this keeps a random
	// signed-in worker from changing every credential produced by a feed.
	if caller.PartyID != source.OwnerPartyID {
		if h.d.Permits == nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
				"the source operator assignment is unavailable")
			return "", false
		}
		allowed, err := h.d.Permits(r.Context(), caller.PartyID, "work-definition-source-owner", contextID)
		if err != nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable",
				"the source operator assignment could not be checked")
			return "", false
		}
		if !allowed {
			httpx.WriteError(w, http.StatusForbidden, "not_permitted",
				"the caller is not the registered source owner or an assigned source operator")
			return "", false
		}
	}
	return caller.PartyID, true
}

func (h *handlers) assessments(w http.ResponseWriter, r *http.Request) {
	// The current view of a project's sources: a project read, answered to a
	// caller who may read that project's evidence (#102). Rows assessed
	// before scoping (no context) apply to every project and ride along.
	if !identity.Authenticated(w, r, h.d.Log, true) {
		return
	}
	contextID := strings.TrimSpace(r.URL.Query().Get("contextId"))
	if contextID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"contextId is required: assessments are a project's view of its sources")
		return
	}
	caller := identity.From(r.Context())
	if caller.PartyID == "" {
		httpx.WriteError(w, http.StatusForbidden, "subject_not_enrolled", "the caller is not enrolled in this deployment")
		return
	}
	if h.d.Permits == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "authorization_unavailable", "the registry authorisation check is unavailable")
		return
	}
	permitted, err := h.d.Permits(r.Context(), caller.PartyID, "read-work-evidence", contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "check evidence read authorization", err)
		return
	}
	if !permitted {
		httpx.WriteError(w, http.StatusForbidden, "not_permitted", "the caller may not read evidence in %s", contextID)
		return
	}
	rows, err := h.d.DB.Q().Query(r.Context(),
		`SELECT coalesce(context_id,''), coalesce(system_ref,''), adapter_ref, max_tier, reason, assessed_by, assessed_at
		 FROM source_assessments WHERE context_id = $1 OR context_id IS NULL
		 ORDER BY coalesce(system_ref, adapter_ref), context_id`, contextID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list assessments", err)
		return
	}
	defer rows.Close()
	type row struct {
		ContextID  string    `json:"contextId,omitempty"`
		SystemRef  string    `json:"systemRef,omitempty"`
		AdapterRef string    `json:"adapterRef"`
		MaxTier    int       `json:"maxTier"`
		Reason     string    `json:"reason"`
		AssessedBy string    `json:"assessedByPartyId"`
		AssessedAt time.Time `json:"assessedAt"`
	}
	out, err := store.Collect(rows, func(r store.Row) (row, error) {
		var v row
		return v, r.Scan(&v.ContextID, &v.SystemRef, &v.AdapterRef, &v.MaxTier, &v.Reason, &v.AssessedBy, &v.AssessedAt)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "read assessments", err)
		return
	}
	if out == nil {
		out = []row{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"assessments": out})
}

type presentation struct {
	ID           string
	CredentialID string
	SubjectRef   string
	RequestedBy  string
	Purpose      string
	Scope        string
	Outcome      string
	Tier         *int
	CreatedAt    time.Time
}

func (h *handlers) record(ctx context.Context, p presentation) error {
	return h.d.DB.InTx(ctx, func(tx store.Querier) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO presentations (id, credential_id, subject_ref, requested_by, purpose,
			                           scope, outcome, tier, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			p.ID, nullable(p.CredentialID), nullable(p.SubjectRef), nullable(p.RequestedBy),
			nullable(p.Purpose), p.Scope, p.Outcome, p.Tier, p.CreatedAt)
		return err
	})
}

func (h *handlers) presentations(w http.ResponseWriter, r *http.Request) {
	// "Who checked me" is the worker's answer (W8), and nobody else's to
	// browse: a subject-filtered read answers that worker or their actor, an
	// unfiltered one a signed-in operator (#102). The verify endpoints
	// themselves stay open by design — an offline stranger with a credential
	// is the whole point (§11).
	subject := r.URL.Query().Get("subjectRef")
	if subject == "" {
		// An unfiltered audit read is a cross-worker disclosure. Resolve it to
		// the authenticated caller rather than accepting a global operator list.
		subject = identity.From(r.Context()).PartyID
	}
	if !authorizeParty(w, r, h.d, subject) {
		return
	}
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT id, coalesce(credential_id,''), coalesce(subject_ref,''), coalesce(requested_by,''),
		       coalesce(purpose,''), scope, outcome, tier, created_at
		FROM presentations
		WHERE subject_ref = $1
		ORDER BY created_at, id`, subject)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list presentations", err)
		return
	}
	defer rows.Close()
	type row struct {
		ID           string    `json:"id"`
		CredentialID string    `json:"credentialId"`
		SubjectRef   string    `json:"subjectRef"`
		RequestedBy  string    `json:"requestedByPartyId"`
		Purpose      string    `json:"purpose"`
		Scope        string    `json:"scope"`
		Outcome      string    `json:"outcome"`
		Tier         *int      `json:"tier,omitempty"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	out, err := store.Collect(rows, func(r store.Row) (row, error) {
		var v row
		return v, r.Scan(&v.ID, &v.CredentialID, &v.SubjectRef, &v.RequestedBy, &v.Purpose,
			&v.Scope, &v.Outcome, &v.Tier, &v.CreatedAt)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "read presentations", err)
		return
	}
	if out == nil {
		out = []row{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"presentations": out})
}

func outcomeOf(v Verdict) string {
	switch {
	case v.Valid:
		return "valid"
	case v.Revoked:
		return "withdrawn"
	case !v.SignatureValid:
		return "signature-failed"
	default:
		return "not-valid"
	}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
