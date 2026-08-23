package main

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
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
	"github.com/theflywheel/crest/pkg/strength"
)

func routes(mux *http.ServeMux, d service.Deps) {
	h := &handlers{
		d:            d,
		definitions:  client.New(config.Str("DEFINITIONS_URL", "http://definitions:8080")),
		registry:     client.New(config.Str("REGISTRY_URL", "http://registry:8080")),
		confirmation: client.New(config.Str("CONFIRMATION_URL", "http://confirmation:8080")),
		dediURL:      config.Str("DEDI_URL", ""),
	}
	mux.HandleFunc("POST /v1/verify", h.verify)
	mux.HandleFunc("POST /v1/source-assessments", h.assess)
	mux.HandleFunc("GET /v1/source-assessments", h.assessments)
	mux.HandleFunc("DELETE /v1/source-assessments/{adapterRef}", h.clearAssessment)
	mux.HandleFunc("GET /v1/presentations", h.presentations)
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

	SignatureValid bool `json:"signatureValid"`
	Revoked        bool `json:"revoked"`
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

	revoked, err := h.revoked(ctx, doc, issuerKey)
	if err != nil {
		v.Reasons = append(v.Reasons, "the status list could not be checked: "+err.Error())
		return v, subjectRef, credID
	}
	v.Revoked = revoked
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

	def, err := h.definition(ctx, cred.CredentialSubject.WorkEvent.Definition)
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
	v.TrustChain = append(v.TrustChain, h.definitionLink(ctx, def))

	if !issuerAuthorised(def, issuerID) {
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"%s is not an authorised issuer for %s@%d", issuerID, def.ID, def.Version))
		return v, subjectRef, credID
	}
	// Follows from the definition, so it is checkable exactly when the
	// definition is: the authorised-issuer list is part of the verifier face
	// that gets published.
	v.TrustChain = append(v.TrustChain, issuerLink(v.TrustChain, issuerID, def))

	assessment, err := h.assessmentFor(ctx, cred.CredentialSubject.Provenance.AdapterRef)
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
func (h *handlers) definitionLink(ctx context.Context, def schema.Definition) Link {
	claim := fmt.Sprintf("measured under %s@%d, %s", def.ID, def.Version, def.Activity.Label)

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

// issuerKey resolves the verification key.
//
// In this deployment that is a call to confirmation. A field verifier does it
// differently — they hold the key already, which is what makes W6's offline
// verification possible; pkg/credential.Verify takes the key as an argument
// precisely so that path exists.
func (h *handlers) issuerKey(ctx context.Context, doc map[string]any) (string, string, error) {
	issuerID, _ := doc["issuer"].(string)
	var info struct {
		Issuer             string `json:"issuer"`
		PublicKeyMultibase string `json:"publicKeyMultibase"`
	}
	if err := h.confirmation.Get(ctx, "/v1/issuer", &info); err != nil {
		return "", issuerID, err
	}
	if issuerID != "" && info.Issuer != issuerID {
		return "", issuerID, fmt.Errorf("this deployment does not hold a key for %s", issuerID)
	}
	return info.PublicKeyMultibase, info.Issuer, nil
}

// revoked fetches the whole status list and reads one bit of it.
//
// The whole list, on purpose: asking about one credential tells the issuer
// which credential is being checked, and the point of a bitstring is that it
// does not (§9).
func (h *handlers) revoked(ctx context.Context, doc map[string]any, issuerKey string) (bool, error) {
	status, ok := doc["credentialStatus"].(map[string]any)
	if !ok {
		return false, errors.New("the credential carries no status entry")
	}
	indexText, _ := status["statusListIndex"].(string)
	var index int
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
		return false, fmt.Errorf("status index %q is not a number", indexText)
	}

	var listDoc map[string]any
	if err := h.confirmation.Get(ctx, "/v1/status-list", &listDoc); err != nil {
		return false, err
	}
	// The list is signed, and it is checked. Otherwise whoever answers the URL
	// decides who has been revoked.
	if err := credential.Verify(listDoc, issuerKey); err != nil {
		return false, fmt.Errorf("the status list is not signed by this issuer: %w", err)
	}
	subject, _ := listDoc["credentialSubject"].(map[string]any)
	encoded, _ := subject["encodedList"].(string)
	list, err := credential.DecodeStatusList(encoded)
	if err != nil {
		return false, err
	}
	return list.Revoked(index), nil
}

func (h *handlers) definition(ctx context.Context, ref schema.VersionedRef) (schema.Definition, error) {
	var def schema.Definition
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s?version=%d",
		url.PathEscape(ref.ID), ref.Version), &def)
	return def, err
}

func (h *handlers) assurance(ctx context.Context, partyID string) schema.IdentityAssurance {
	var out struct {
		IdentityAssurance schema.IdentityAssurance `json:"identityAssurance"`
	}
	if err := h.registry.Get(ctx,
		"/v1/parties/"+url.PathEscape(partyID)+"/assurance", &out); err != nil {
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
	var req struct {
		AdapterRef string `json:"adapterRef"`
		MaxTier    int    `json:"maxTier"`
		Reason     string `json:"reason"`
		AssessedBy string `json:"assessedByPartyId"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.AdapterRef == "" || req.Reason == "" || req.AssessedBy == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"downgrading a source needs the source, a reason and whoever decided it")
		return
	}
	if req.MaxTier < 0 || req.MaxTier > 3 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "maxTier is 0 to 3")
		return
	}
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO source_assessments (adapter_ref, max_tier, reason, assessed_by, assessed_at)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (adapter_ref) DO UPDATE
			SET max_tier = EXCLUDED.max_tier, reason = EXCLUDED.reason,
			    assessed_by = EXCLUDED.assessed_by, assessed_at = EXCLUDED.assessed_at`,
			req.AdapterRef, req.MaxTier, req.Reason, req.AssessedBy, h.d.Clock.Now())
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
	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		_, err := tx.Exec(r.Context(),
			`DELETE FROM source_assessments WHERE adapter_ref = $1`, r.PathValue("adapterRef"))
		return err
	}); err != nil {
		httpx.Fail(w, h.d.Log, "clear source assessment", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) assessmentFor(ctx context.Context, adapterRef string) (*strength.SourceAssessment, error) {
	var maxTier int
	var reason string
	err := h.d.DB.Q().QueryRow(ctx,
		`SELECT max_tier, reason FROM source_assessments WHERE adapter_ref = $1`, adapterRef).
		Scan(&maxTier, &reason)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil // no concerns recorded
	}
	if err != nil {
		return nil, err
	}
	return &strength.SourceAssessment{MaxTier: maxTier, Reason: reason}, nil
}

func (h *handlers) assessments(w http.ResponseWriter, r *http.Request) {
	rows, err := h.d.DB.Q().Query(r.Context(),
		`SELECT adapter_ref, max_tier, reason, assessed_by, assessed_at
		 FROM source_assessments ORDER BY adapter_ref`)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list assessments", err)
		return
	}
	defer rows.Close()
	type row struct {
		AdapterRef string    `json:"adapterRef"`
		MaxTier    int       `json:"maxTier"`
		Reason     string    `json:"reason"`
		AssessedBy string    `json:"assessedByPartyId"`
		AssessedAt time.Time `json:"assessedAt"`
	}
	out, err := store.Collect(rows, func(r store.Row) (row, error) {
		var v row
		return v, r.Scan(&v.AdapterRef, &v.MaxTier, &v.Reason, &v.AssessedBy, &v.AssessedAt)
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
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT id, coalesce(credential_id,''), coalesce(subject_ref,''), coalesce(requested_by,''),
		       coalesce(purpose,''), scope, outcome, tier, created_at
		FROM presentations
		WHERE ($1 = '' OR subject_ref = $1)
		ORDER BY created_at, id`, r.URL.Query().Get("subjectRef"))
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
