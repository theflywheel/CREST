package verification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Issuance lives here, in the credential substrate, since #137.
//
// It used to live in the confirmation service, which meant the payments
// application held the issuer keys and signed credentials — a substrate
// function owned by the application layer. Now the application requests
// issuance ("this claim's record is confirmed — issue") and everything about
// what a credential is — its shape, its keys, its status list, its revocation
// — is answered on this side of the infrastructure boundary. The confirmation
// service keeps zero knowledge of any of it.

// issueRequest is the whole of what the payments application may say about a
// credential: which confirmed claim, whose, by which route, when. The unit is
// read from evidence here rather than accepted from the caller, so what is
// signed is what the record says, not what the application relayed.
type issueRequest struct {
	ClaimID   string    `json:"claimId"`
	UnitID    string    `json:"unitId"`
	PartyID   string    `json:"partyId"`
	ContextID string    `json:"contextId"`
	Route     string    `json:"route"`
	At        time.Time `json:"at"`
}

type issuedCredential struct {
	ID          string          `json:"id"`
	ClaimID     string          `json:"claimId"`
	SubjectRef  string          `json:"subjectRef"`
	StatusIndex int             `json:"statusIndex"`
	Digest      string          `json:"digest"`
	Doc         json.RawMessage `json:"credential"`
	IssuedAt    time.Time       `json:"issuedAt"`
	RevokedAt   *time.Time      `json:"revokedAt,omitempty"`

	// Carried between building and signing, never stored: the credential is
	// assembled before its status slot is known, and signed after.
	unit  schema.Unit `json:"-"`
	route string      `json:"-"`

	// Resolved once, at build time, from the definitions service. Nil where
	// there is nothing a verifier could check.
	defProof  *schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof `json:"-"`
	skillCode *string                                                              `json:"-"`
	authority *schema.WorkEventCredentialCredentialSubjectIssuerAuthority          `json:"-"`
}

// issue signs one credential for one confirmed claim, exactly once.
//
// Idempotent by claim: the application's exit can crash between this call
// succeeding and its own record committing, and its retry must get the same
// credential back rather than a second one — two credentials for one claim
// would be two status slots for one fact, and revoking one would leave the
// other standing.
func (h *handlers) issue(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if !httpx.ReadJSON(w, r, &req) {
		return
	}
	if req.ClaimID == "" || req.UnitID == "" || req.PartyID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"claimId, unitId and partyId are all required; a credential must say which fact it asserts and about whom")
		return
	}
	if existing, err := credentialByClaim(r.Context(), h.d.DB.Q(), req.ClaimID); err == nil {
		httpx.WriteJSON(w, http.StatusOK, existing)
		return
	}

	c, err := h.buildCredential(r.Context(), req)
	if err != nil {
		httpx.Fail(w, h.d.Log, "build credential", err)
		return
	}
	err = h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		idx, err := nextStatusIndex(r.Context(), tx)
		if err != nil {
			return err
		}
		// The index is allocated inside the transaction and then written into
		// the credential, so no two credentials can share a slot — revoking
		// one would otherwise revoke the other.
		if err := c.setStatusIndex(idx, h.statusListURL, h.issuer, req.At); err != nil {
			return err
		}
		return insertCredential(r.Context(), tx, *c)
	})
	if err != nil {
		// The one race the claim_id UNIQUE constraint exists for: two exits of
		// the same claim arriving together. The loser reads the winner's.
		if existing, readErr := credentialByClaim(r.Context(), h.d.DB.Q(), req.ClaimID); readErr == nil {
			httpx.WriteJSON(w, http.StatusOK, existing)
			return
		}
		httpx.Fail(w, h.d.Log, "store credential", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

// buildCredential assembles the credential. It reads the unit from evidence
// rather than accepting it from the caller, so what is signed is what the
// record currently says rather than what the application relayed.
func (h *handlers) buildCredential(ctx context.Context, req issueRequest) (*issuedCredential, error) {
	var unit schema.Unit
	if err := h.evidence.Get(ctx, "/internal/units/"+req.UnitID, &unit); err != nil {
		return nil, fmt.Errorf("could not read unit %s: %w", req.UnitID, err)
	}

	// The subject is the Party's own pairwise, deployment-local DID (§4). Not a
	// name, not a national identifier, not the provider's subject — nothing
	// that correlates outside this deployment (W8, W9).
	return &issuedCredential{
		ID:         id.New(h.d.Clock, "credential"),
		ClaimID:    req.ClaimID,
		SubjectRef: req.PartyID,
		IssuedAt:   req.At,
		unit:       unit,
		route:      req.Route,
		defProof:   h.definitionProof(ctx, unit.Definition),
		skillCode:  h.skillCodeOf(ctx, unit.Definition),
		authority:  h.issuerAuthority(ctx, req.ContextID),
	}, nil
}

// skillCodeOf reads the skill the definition evidences, so the credential can
// carry it directly (#16).
//
// Copied into the credential rather than left as a reference, because it is the
// one field whose entire purpose is to be read by somebody who is not this
// deployment — and a verifier who must resolve the definition first to learn
// what skill this was cannot answer the question offline, which is the case
// that matters.
func (h *handlers) skillCodeOf(ctx context.Context, ref schema.VersionedRef) *string {
	var def schema.Definition
	if err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s?version=%d",
		url.PathEscape(ref.ID), ref.Version), &def); err != nil {
		h.d.Log.Warn("could not read the definition's skill code",
			"definition", ref.ID, "version", ref.Version, "error", err)
		return nil
	}
	return def.SkillCode
}

// issuerAuthority resolves the chain a verifier walks up to somebody answerable
// (#16, Blueprint §3 and §8).
//
// §8's sketch named qualificationRef and grantRef as two things. §2 had already
// collapsed them into one Authorization at two scopes — instance-wide was the
// old Qualification, context-bound the old ProjectGrant — so this resolves the
// same primitive twice rather than two primitives once.
//
// Every reference is to an ORGANISATION's authorization. A person's is never
// published (#68) because it would be a permanent public record of who works
// where, so a chain ending at a supervisor would end at something a verifier
// cannot resolve — which is worse than ending nowhere, because it looks like it
// leads somewhere.
//
// Best-effort, like the definition pin and for the same reason: this is on the
// path that releases payment.
func (h *handlers) issuerAuthority(ctx context.Context,
	contextID string) *schema.WorkEventCredentialCredentialSubjectIssuerAuthority {
	var inst struct {
		Instance struct {
			OperatorPartyID string `json:"operatorPartyId"`
		} `json:"instance"`
	}
	if err := h.registry.Get(ctx, "/v1/instance", &inst); err != nil ||
		inst.Instance.OperatorPartyID == "" {
		// Without an operator there is no authority to name. Nil rather than a
		// half-filled object: a chain with no root is not a shorter chain, it
		// is not a chain.
		h.d.Log.Warn("could not read this deployment's operator; issuing without an issuer authority", "error", err)
		return nil
	}
	out := &schema.WorkEventCredentialCredentialSubjectIssuerAuthority{
		OrgID: inst.Instance.OperatorPartyID,
	}
	if id := h.firstAuthorization(ctx, inst.Instance.OperatorPartyID, "instance", ""); id != "" {
		out.QualificationRef = &id
	}
	if id := h.firstAuthorization(ctx, inst.Instance.OperatorPartyID, "context", contextID); id != "" {
		out.GrantRef = &id
	}
	return out
}

// firstAuthorization returns the id of one active authorization at a scope, or
// empty. Deterministic by id order, so two credentials issued a second apart
// name the same one.
func (h *handlers) firstAuthorization(ctx context.Context, partyID, scope, contextID string) string {
	q := url.Values{"partyId": {partyID}, "scope": {scope}}
	if contextID != "" {
		q.Set("contextId", contextID)
	}
	var out struct {
		Authorizations []struct {
			ID string `json:"id"`
		} `json:"authorizations"`
	}
	// The service twin, not the caller-facing route: this is service traffic on
	// the issuance path, and /v1/authorizations answers signed-in callers only.
	if err := h.registry.Get(ctx, "/internal/authorizations?"+q.Encode(), &out); err != nil {
		h.d.Log.Warn("could not read the issuing organisation's authorizations",
			"party", partyID, "scope", scope, "error", err)
		return ""
	}
	if len(out.Authorizations) == 0 {
		return ""
	}
	return out.Authorizations[0].ID
}

// definitionProof resolves where a verifier can check the definition version
// this credential was measured under (#16).
//
// Best-effort, and that is a deliberate choice rather than laziness. This runs
// on the path that releases a worker's payment, and every confirmation-window
// exit releases payment — so a definitions service that is slow or down must
// not be able to stop a credential being issued. The consequence of failing
// here is a credential a verifier has to trust the issuer about, which is
// exactly what CREST was before #69; the consequence of failing hard would be
// a worker not paid because a lookup timed out.
//
// A nil result is therefore ambiguous between "no transparency log" and "could
// not reach definitions", and that ambiguity is the price. It is logged so the
// second case is visible to an operator rather than silently indistinguishable.
func (h *handlers) definitionProof(ctx context.Context,
	ref schema.VersionedRef) *schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof {
	var pub struct {
		Namespace       string `json:"namespace"`
		Registry        string `json:"registry"`
		Record          string `json:"record"`
		RegistryVersion string `json:"registryVersion"`
		Digest          string `json:"digest"`
		Transparent     bool   `json:"transparent"`
	}
	err := h.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s/publication?version=%d",
		url.PathEscape(ref.ID), ref.Version), &pub)
	switch {
	case client.Code(err) == http.StatusNotFound:
		// Not published. Normal on a deployment with no node, and normal
		// briefly right after activation.
		return nil
	case err != nil:
		h.d.Log.Warn("could not read the definition's publication; issuing without a resolvable pin",
			"definition", ref.ID, "version", ref.Version, "error", err)
		return nil
	case !pub.Transparent:
		// Published to the Postgres fallback. There is a record, and no proof —
		// so there is nothing to point a verifier at, and pointing them at it
		// anyway would dress up "trust us" as "check this".
		return nil
	}
	return &schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof{
		Namespace: pub.Namespace,
		Registry:  pub.Registry,
		Record:    pub.Record,
		Version:   pub.RegistryVersion,
		Digest:    pub.Digest,
	}
}

// evidenceFieldsOf lists what the source record carried, by name.
//
// A verifier offline in a field office cannot ask CREST which fields the record
// had, and a definition's tier map can require one. Without this the offline
// answer is systematically weaker than the online answer — which is the wrong
// way round, because offline is the case W6 exists for.
//
// Sorted, so two credentials over the same record produce the same bytes.
func evidenceFieldsOf(unit schema.Unit) []string {
	fields := make([]string, 0, len(unit.Enrichment)+1)
	for name := range unit.Enrichment {
		fields = append(fields, name)
	}
	if unit.Geography != nil {
		fields = append(fields, "geography")
	}
	sort.Strings(fields)
	return fields
}

// setStatusIndex finishes the credential once its status slot is known, then
// signs it. Signing last is what makes the status entry part of what is signed.
func (c *issuedCredential) setStatusIndex(idx int, listURL string, iss *credential.Issuer, now time.Time) error {
	doc, err := credential.Document(credential.Subject{
		CredentialID:    c.ID,
		IssuerID:        iss.ID(),
		SubjectRef:      c.SubjectRef,
		ClaimID:         c.ClaimID,
		Unit:            c.unit,
		Activity:        c.unit.Definition.ID,
		SkillCode:       c.skillCode,
		IssuerAuthority: c.authority,
		Confirmation:    schema.ClaimConfirmationRoute(c.route),
		ConfirmedAt:     now,
		EvidenceFields:  evidenceFieldsOf(c.unit),
		DefinitionProof: c.defProof,
		StatusListURL:   listURL,
		StatusListIndex: idx,
		ValidFrom:       now,
	})
	if err != nil {
		return fmt.Errorf("the credential does not satisfy its own schema: %w", err)
	}
	signed, err := iss.Issue(doc, now)
	if err != nil {
		return err
	}
	digest, err := credential.Digest(signed)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		return err
	}
	c.StatusIndex = idx
	c.Digest = digest
	c.Doc = raw
	return nil
}

// ── The credential record ───────────────────────────────────────────────────

func insertCredential(ctx context.Context, tx store.Querier, c issuedCredential) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO credentials (id, claim_id, subject_ref, status_index, digest, doc, issued_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		c.ID, c.ClaimID, c.SubjectRef, c.StatusIndex, c.Digest, c.Doc, c.IssuedAt)
	return err
}

const credentialColumns = `id, claim_id, subject_ref, status_index, digest, doc, issued_at, revoked_at`

func scanCredential(r store.Row) (issuedCredential, error) {
	var c issuedCredential
	err := r.Scan(&c.ID, &c.ClaimID, &c.SubjectRef, &c.StatusIndex, &c.Digest, &c.Doc, &c.IssuedAt, &c.RevokedAt)
	return c, err
}

func getCredential(ctx context.Context, q store.Querier, credID string) (issuedCredential, error) {
	return scanCredential(q.QueryRow(ctx,
		`SELECT `+credentialColumns+` FROM credentials WHERE id = $1`, credID))
}

func credentialByClaim(ctx context.Context, q store.Querier, claimID string) (issuedCredential, error) {
	return scanCredential(q.QueryRow(ctx,
		`SELECT `+credentialColumns+` FROM credentials WHERE claim_id = $1`, claimID))
}

// nextStatusIndex hands out the next slot in the bitstring, inside the caller's
// transaction so two issuances cannot take the same one. Two credentials
// sharing a status index means revoking one revokes both.
func nextStatusIndex(ctx context.Context, tx store.Querier) (int, error) {
	var idx int
	err := tx.QueryRow(ctx,
		`UPDATE status_list SET next_index = next_index + 1 WHERE id = 1 RETURNING next_index - 1`).
		Scan(&idx)
	return idx, err
}

func loadStatusList(ctx context.Context, q store.Querier) (*credential.StatusList, error) {
	var bits []byte
	if err := q.QueryRow(ctx, `SELECT bits FROM status_list WHERE id = 1`).Scan(&bits); err != nil {
		return nil, err
	}
	return credential.FromBytes(bits), nil
}

func saveStatusList(ctx context.Context, tx store.Querier, list *credential.StatusList) error {
	_, err := tx.Exec(ctx, `UPDATE status_list SET bits = $1 WHERE id = 1`, list.Bytes())
	return err
}

func revokeCredential(ctx context.Context, tx store.Querier, credID string, at time.Time) (int, error) {
	var idx int
	err := tx.QueryRow(ctx,
		`UPDATE credentials SET revoked_at = $2 WHERE id = $1 RETURNING status_index`, credID, at).
		Scan(&idx)
	return idx, err
}

// credentialsFor lists the signed credentials issued to a set of party ids —
// in practice, one person across a merge (#104). Revoked credentials are
// included: a withdrawn credential is part of what happened, and the verifier
// checking it against the status list is the mechanism for finding that out;
// filtering it here would make this listing quietly disagree with the list.
func credentialsFor(ctx context.Context, q store.Querier, partyIDs []string) ([]json.RawMessage, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM credentials
		 WHERE subject_ref = ANY($1)
		 ORDER BY issued_at, id`, partyIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (json.RawMessage, error) {
		var doc []byte
		return doc, r.Scan(&doc)
	})
}

// ── The caller-facing surface ───────────────────────────────────────────────

// listCredentials answers "every credential issued to this person", following
// any merge the same way every other party-scoped read does (#100, #104). The
// response is credentials and nothing else — deliberately not the identifier
// list, not which id each credential was issued under beyond what the
// credential itself says, and no marker that a merge happened.
func (h *handlers) listCredentials(w http.ResponseWriter, r *http.Request) {
	// The worker's own wallet view (#102). A verifier's window into the chain
	// is deliberately through /v1/parties/{id}/credentials, where each look
	// writes a presentation entry, never through this list silently.
	if r.URL.Query().Get("partyId") == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"partyId is required: this endpoint answers what was issued to one worker")
		return
	}
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	if _, ok := identity.Authorize(w, r, h.d.Log, ids[0], "",
		h.d.Authenticating, h.d.Permits); !ok {
		return
	}
	h.listCredentialsRaw(w, r)
}

func (h *handlers) listCredentialsRaw(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("partyId") == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_parameter",
			"partyId is required: this endpoint answers what was issued to one worker")
		return
	}
	ids, ok := sameParty(w, r, h.d)
	if !ok {
		return
	}
	creds, err := credentialsFor(r.Context(), h.d.DB.Q(), ids)
	if err != nil {
		httpx.Fail(w, h.d.Log, "list credentials", err)
		return
	}
	if creds == nil {
		creds = []json.RawMessage{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"credentials": creds, "count": len(creds)})
}

func (h *handlers) getCredentialByID(w http.ResponseWriter, r *http.Request) {
	c, err := getCredential(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

// revoke flips one bit. Withdrawal is the single central fact about credentials
// (§9), and this is the whole of it.
func (h *handlers) revoke(w http.ResponseWriter, r *http.Request) {
	// Withdrawal is the issuer's act (§9). Until issuer roles are modelled,
	// the gate is a signed-in caller — recorded, not anonymous (#102).
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	h.revokeAndAnswer(w, r)
}

// revokeInternal is the service twin (#102): the payments application revokes
// a crash-orphaned credential at a dispute exit — a credential that exists for
// a window no confirming exit ever committed. Service traffic, no caller
// token, same operation.
func (h *handlers) revokeInternal(w http.ResponseWriter, r *http.Request) {
	h.revokeAndAnswer(w, r)
}

func (h *handlers) revokeAndAnswer(w http.ResponseWriter, r *http.Request) {
	now := h.d.Clock.Now()
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		idx, err := revokeCredential(r.Context(), tx, r.PathValue("id"), now)
		if err != nil {
			return err
		}
		list, err := loadStatusList(r.Context(), tx)
		if err != nil {
			return err
		}
		if err := list.Revoke(idx); err != nil {
			return err
		}
		return saveStatusList(r.Context(), tx, list)
	})
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// credentialForClaim answers whether a credential exists for one claim — the
// service twin the dispute exit reads to find a crash's orphan.
func (h *handlers) credentialForClaim(w http.ResponseWriter, r *http.Request) {
	c, err := credentialByClaim(r.Context(), h.d.DB.Q(), r.PathValue("claimId"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

// statusList returns the whole bitstring, signed. Whole, because a verifier who
// asks about one credential tells the issuer which credential they are checking.
func (h *handlers) statusList(w http.ResponseWriter, r *http.Request) {
	list, err := loadStatusList(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "load status list", err)
		return
	}
	doc, err := h.issuer.StatusListCredential(h.statusListURL, list, h.d.Clock.Now())
	if err != nil {
		httpx.Fail(w, h.d.Log, "sign status list", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, doc)
}

// issuerInfo publishes the verification key. A verifier needs it once and then
// never again — which is what makes offline verification possible.
func (h *handlers) issuerInfo(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"issuer":             h.issuer.ID(),
		"verificationMethod": h.issuer.VerificationMethod(),
		"publicKeyMultibase": h.issuer.PublicKeyMultibase(),
		"cryptosuite":        credential.CryptosuiteName,
	})
}
