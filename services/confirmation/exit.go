package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// The four exits from a T=7 window, in one function, on purpose.
//
// W4 says every exit releases payment: confirm, dispute, auto-confirm,
// supervisor-assisted. The cheapest way for that to break is for the four to be
// four code paths, three of which release and one of which — the one nobody
// demos — does not. So there is one path, the route is a parameter, and the
// release is unconditional.
//
// What the route *does* change is the record:
//
//	self, auto, assisted → the claim is ACCEPTED and a credential is issued
//	dispute              → the claim is DISPUTED and no credential is issued
//
// Both branches release payment. A credential asserting a claim the worker
// disputes would be a false statement CREST signed; withholding the money
// because the worker objected would be a punishment for objecting.

type exiter struct {
	db            *store.DB
	evidence      *client.Client
	definitions   *client.Client
	registry      *client.Client
	issuer        *credential.Issuer
	statusListURL string
	log           *slog.Logger
	clock         interface{ Now() time.Time }
}

type exitResult struct {
	Window     Window            `json:"window"`
	Credential *issuedCredential `json:"credential,omitempty"`
}

type releaseRequest struct {
	ClaimID    string    `json:"claimId"`
	UnitID     string    `json:"unitId"`
	PartyID    string    `json:"partyId"`
	ReleasedBy string    `json:"releasedBy"`
	ReleasedAt time.Time `json:"releasedAt"`
}

func (e *exiter) exit(ctx context.Context, claimID, route string) (exitResult, error) {
	now := e.clock.Now()

	w, err := getWindow(ctx, e.db.Q(), claimID, false)
	if err != nil {
		return exitResult{}, err
	}
	if !w.Open() {
		// Already exited. Idempotent rather than an error: the sweep and a
		// worker's confirmation can race, and the worker should win without
		// anyone seeing a failure.
		//
		// A dispute is the exception, and W3 is why. Silence is not consent
		// against the worker: the seven days are a window for objecting, not a
		// deadline for noticing, so a claim that auto-confirmed must still be
		// disputable afterwards. The payment is already out and stays out —
		// what changes is what the record says.
		if route == routeDispute {
			if err := e.transitionClaim(ctx, claimID, schema.ClaimStateDISPUTED, route); err != nil {
				return exitResult{}, err
			}
		}
		var cred *issuedCredential
		if w.CredentialID != nil {
			c, err := getCredential(ctx, e.db.Q(), *w.CredentialID)
			if err == nil {
				cred = &c
			}
		}
		return exitResult{Window: w, Credential: cred}, nil
	}

	// The claim's state is owned by evidence, so it moves there first. If this
	// fails, nothing here has changed and the caller can retry.
	target := schema.ClaimStateACCEPTED
	if route == routeDispute {
		target = schema.ClaimStateDISPUTED
	}
	if err := e.transitionClaim(ctx, claimID, target, route); err != nil {
		return exitResult{}, err
	}

	var issued *issuedCredential
	if route != routeDispute {
		issued, err = e.buildCredential(ctx, w, route, now)
		if err != nil {
			return exitResult{}, err
		}
	}

	err = e.db.InTx(ctx, func(tx store.Querier) error {
		var credID *string
		if issued != nil {
			idx, err := nextStatusIndex(ctx, tx)
			if err != nil {
				return err
			}
			// The index is allocated inside the transaction and then written
			// into the credential, so no two credentials can share a slot —
			// revoking one would otherwise revoke the other.
			if err := issued.setStatusIndex(idx, e.statusListURL, e.issuer, now); err != nil {
				return err
			}
			if err := insertCredential(ctx, tx, *issued); err != nil {
				return err
			}
			credID = &issued.ID
		}
		if err := recordExit(ctx, tx, claimID, route, now, credID); err != nil {
			return err
		}
		// The release is enqueued in the same transaction as the exit. A crash
		// between them is the failure W4 cannot survive, and this is what makes
		// it impossible rather than unlikely.
		return store.Enqueue(ctx, tx, topicPaymentRelease, releaseRequest{
			ClaimID: w.ClaimID, UnitID: w.UnitID, PartyID: w.PartyID,
			ReleasedBy: route, ReleasedAt: now,
		})
	})
	if err != nil {
		return exitResult{}, err
	}

	w, err = getWindow(ctx, e.db.Q(), claimID, false)
	if err != nil {
		return exitResult{}, err
	}
	return exitResult{Window: w, Credential: issued}, nil
}

func (e *exiter) transitionClaim(ctx context.Context, claimID string, to schema.ClaimState, route string) error {
	body := map[string]any{"to": to}
	if route != routeDispute {
		body["route"] = route
	}
	if err := e.evidence.Post(ctx, "/internal/claims/"+claimID+"/transition", body, nil); err != nil {
		return fmt.Errorf("evidence would not move claim %s to %s: %w", claimID, to, err)
	}
	return nil
}

// buildCredential assembles and signs the credential. It reads the unit from
// evidence rather than caching it, so what is signed is what the record
// currently says rather than what it said when the window opened.
func (e *exiter) buildCredential(ctx context.Context, w Window, route string, now time.Time) (*issuedCredential, error) {
	var unit schema.Unit
	if err := e.evidence.Get(ctx, "/internal/units/"+w.UnitID, &unit); err != nil {
		return nil, fmt.Errorf("could not read unit %s: %w", w.UnitID, err)
	}

	// The subject is the Party's own pairwise, deployment-local DID (§4). Not a
	// name, not a national identifier, not the provider's subject — nothing
	// that correlates outside this deployment (W8, W9).
	subjectRef := w.PartyID

	return &issuedCredential{
		ID:         id.New(e.clock, "credential"),
		ClaimID:    w.ClaimID,
		SubjectRef: subjectRef,
		IssuedAt:   now,
		unit:       unit,
		route:      route,
		defProof:   e.definitionProof(ctx, unit.Definition),
		skillCode:  e.skillCodeOf(ctx, unit.Definition),
		authority:  e.issuerAuthority(ctx, unit.ContextID),
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
func (e *exiter) skillCodeOf(ctx context.Context, ref schema.VersionedRef) *string {
	var def schema.Definition
	if err := e.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s?version=%d",
		url.PathEscape(ref.ID), ref.Version), &def); err != nil {
		e.log.Warn("could not read the definition's skill code",
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
func (e *exiter) issuerAuthority(ctx context.Context,
	contextID string) *schema.WorkEventCredentialCredentialSubjectIssuerAuthority {
	var inst struct {
		Instance struct {
			OperatorPartyID string `json:"operatorPartyId"`
		} `json:"instance"`
	}
	if err := e.registry.Get(ctx, "/v1/instance", &inst); err != nil ||
		inst.Instance.OperatorPartyID == "" {
		// Without an operator there is no authority to name. Nil rather than a
		// half-filled object: a chain with no root is not a shorter chain, it
		// is not a chain.
		e.log.Warn("could not read this deployment's operator; issuing without an issuer authority", "error", err)
		return nil
	}
	out := &schema.WorkEventCredentialCredentialSubjectIssuerAuthority{
		OrgID: inst.Instance.OperatorPartyID,
	}
	if id := e.firstAuthorization(ctx, inst.Instance.OperatorPartyID, "instance", ""); id != "" {
		out.QualificationRef = &id
	}
	if id := e.firstAuthorization(ctx, inst.Instance.OperatorPartyID, "context", contextID); id != "" {
		out.GrantRef = &id
	}
	return out
}

// firstAuthorization returns the id of one active authorization at a scope, or
// empty. Deterministic by id order, so two credentials issued a second apart
// name the same one.
func (e *exiter) firstAuthorization(ctx context.Context, partyID, scope, contextID string) string {
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
	if err := e.registry.Get(ctx, "/internal/authorizations?"+q.Encode(), &out); err != nil {
		e.log.Warn("could not read the issuing organisation's authorizations",
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
// on the path that releases a worker's payment, and every T=7 exit releases
// payment — so a definitions service that is slow or down must not be able to
// stop a credential being issued. The consequence of failing here is a
// credential a verifier has to trust the issuer about, which is exactly what
// CREST was before #69; the consequence of failing hard would be a worker not
// paid because a lookup timed out.
//
// A nil result is therefore ambiguous between "no transparency log" and "could
// not reach definitions", and that ambiguity is the price. It is logged so the
// second case is visible to an operator rather than silently indistinguishable.
func (e *exiter) definitionProof(ctx context.Context,
	ref schema.VersionedRef) *schema.WorkEventCredentialCredentialSubjectWorkEventDefinitionProof {
	var pub struct {
		Namespace       string `json:"namespace"`
		Registry        string `json:"registry"`
		Record          string `json:"record"`
		RegistryVersion string `json:"registryVersion"`
		Digest          string `json:"digest"`
		Transparent     bool   `json:"transparent"`
	}
	err := e.definitions.Get(ctx, fmt.Sprintf("/v1/definitions/%s/publication?version=%d",
		url.PathEscape(ref.ID), ref.Version), &pub)
	switch {
	case client.Code(err) == http.StatusNotFound:
		// Not published. Normal on a deployment with no node, and normal
		// briefly right after activation.
		return nil
	case err != nil:
		e.log.Warn("could not read the definition's publication; issuing without a resolvable pin",
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

// deliverNotification sends a notification and records whether it reached the
// worker.
//
// notify deliberately answers 201 whether the send succeeded, failed, or found
// no route at all — returning an error there would have the relay redeliver and
// write the row again. That is correct for the outbox and wrong for the sweep,
// which until now could not tell the difference and auto-confirmed regardless.
//
// So the outcome comes back in the body and is written onto the window. A
// window marked unreached is never auto-confirmed; it is surfaced, the same way
// a held payment is surfaced, because the alternative is a worker whose record
// was confirmed against them during a silence the system produced.
func deliverNotification(ctx context.Context, d service.Deps, notify *client.Client,
	payload json.RawMessage) error {
	var req struct {
		ClaimID string `json:"claimId"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return err
	}
	var out struct {
		State   string `json:"state"`
		Channel string `json:"channel"`
	}
	if err := notify.Do(ctx, "POST", "/internal/notifications", payload, &out); err != nil {
		return err
	}

	reach, detail := "reached", out.Channel
	if out.State != "SENT" {
		reach = "unreached"
		detail = out.State
		if out.Channel != "" && out.Channel != "none" {
			detail += " on " + out.Channel
		}
	}
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		return recordReach(ctx, tx, req.ClaimID, reach, detail)
	})
}
