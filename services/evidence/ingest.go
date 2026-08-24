package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/theflywheel/crest/adapters"
	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// ingest turns a parsed batch into units and claims.
//
// The order of operations is the design:
//
//  1. The submitter must be authorised for this function in this context. An
//     unauthorised batch is refused whole, because a partially-accepted batch
//     from someone who should not be submitting is worse than none.
//  2. The definition version in force is resolved once and pinned onto every
//     unit. Pinning at ingestion is what lets the definition be versioned later
//     without stranding anything (§6, §7).
//  3. Each row is validated against the canonical evidence schema before
//     anything is written. An adapter that emits a bad record is a bug in the
//     adapter, and the pipeline validating provenance rather than trusting it
//     is §8's rule.
//  4. The worker is resolved. Exactly one match becomes a claim; no match and
//     an ambiguous match both become unclear rows, and neither becomes a guess.
//
// A unit is created only for a row that produced a claim. That is a choice
// worth stating: a unit with no claim is a defensible thing to want — the work
// did happen — but it would let an unattributed row look like recorded work in
// every count, while no worker can ever be paid for it. Keeping it in the
// unclear queue keeps it visible as *unfinished*, which is what it is.
type ingestor struct {
	registry    *client.Client
	definitions *client.Client
	clock       interface{ Now() time.Time }
}

type ingestParams struct {
	ContextID    string
	DefinitionID string
	SubmittedBy  string
	Source       adapters.Source
}

type ingestResult struct {
	Batch   Batch        `json:"batch"`
	Claims  []string     `json:"claimIds"`
	Unclear []UnclearRow `json:"unclear"`
}

func (in *ingestor) run(ctx context.Context, db *store.DB, p ingestParams,
	rows []adapters.Row, rejections []adapters.Rejection) (ingestResult, error) {
	now := in.clock.Now()

	permitted, err := in.permits(ctx, p)
	if err != nil {
		return ingestResult{}, err
	}
	if !permitted {
		return ingestResult{}, fmt.Errorf("%s is not authorised to submit evidence in %s",
			p.SubmittedBy, p.ContextID)
	}

	def, err := in.definition(ctx, p.DefinitionID)
	if err != nil {
		return ingestResult{}, err
	}
	if def.State != schema.DefinitionStateACTIVE {
		return ingestResult{}, fmt.Errorf("definition %s is %s, not ACTIVE: evidence may only be "+
			"submitted against a definition in force", def.ID, def.State)
	}

	batch := Batch{
		ID:                id.New(in.clock, "batch"),
		ContextID:         p.ContextID,
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		SubmittedBy:       p.SubmittedBy,
		AdapterRef:        p.Source.SystemRef,
		RowsTotal:         len(rows) + len(rejections),
		CreatedAt:         now,
	}

	result := ingestResult{Unclear: []UnclearRow{}, Claims: []string{}}
	type pending struct {
		unit      schema.Unit
		claim     schema.Claim
		dedupeKey string
	}
	var accepted []pending

	// Rows the adapter itself refused are unclear rows too. They describe work
	// that may well have happened, and the file is the only record of it.
	for _, rej := range rejections {
		result.Unclear = append(result.Unclear, UnclearRow{
			ID: id.New(in.clock, "unclear"), BatchID: batch.ID,
			RowRef: rej.Ref, Reason: rej.Reason, CreatedAt: now,
		})
	}

	for _, row := range rows {
		reason, unit, claim := in.consider(ctx, row, def, p, now)
		if reason != "" {
			raw, err := json.Marshal(redact(row.Record))
			if err != nil {
				return ingestResult{}, err
			}
			result.Unclear = append(result.Unclear, UnclearRow{
				ID: id.New(in.clock, "unclear"), BatchID: batch.ID,
				RowRef: row.Ref, Reason: reason, Record: raw, CreatedAt: now,
			})
			continue
		}
		accepted = append(accepted, pending{unit: unit, claim: claim, dedupeKey: dedupeKey(unit, row.Record)})
	}

	batch.RowsAccepted = len(accepted)
	batch.RowsUnclear = len(result.Unclear)

	// One transaction for the whole batch, including the outbox messages. A
	// claim that exists without the message that opens its confirmation window
	// is a worker who is never asked and never paid (W2, W4) — so the two
	// cannot be separated by a crash.
	err = db.InTx(ctx, func(tx store.Querier) error {
		if err := insertBatch(ctx, tx, batch); err != nil {
			return err
		}
		// The heartbeat, in the same transaction as the evidence it came with
		// (#22). Written outside it, a rolled-back batch would still leave the
		// source looking alive — and "the feed is fine, we just have no data"
		// is the exact wrong conclusion.
		//
		// A batch that produced only rejections still counts as a heartbeat:
		// this watches whether the source is *sending*, not whether what it
		// sends is any good. Bad rows are the unclear queue's problem and are
		// visible there; silence is invisible everywhere else.
		if err := markSeen(ctx, tx, batch.AdapterRef, batch.ContextID, now); err != nil {
			return err
		}
		for _, u := range result.Unclear {
			if err := insertUnclear(ctx, tx, u); err != nil {
				return err
			}
		}
		for _, a := range accepted {
			unitID, err := insertUnit(ctx, tx, batch.ID, a.unit, a.dedupeKey)
			if err != nil {
				return err
			}
			// The unit already existed: this is the same work arriving again,
			// so the claim is written against the unit that is already there
			// and the uniqueness on (unit_id, party_id) refuses the duplicate.
			a.claim.UnitID = unitID
			a.unit.ID = unitID
			created, err := insertClaim(ctx, tx, a.claim)
			if err != nil {
				return err
			}
			if !created {
				continue // already claimed by an earlier run of the same batch
			}
			result.Claims = append(result.Claims, a.claim.ID)
			if err := store.Enqueue(ctx, tx, topicClaimCreated, windowRequest{
				ClaimID:      a.claim.ID,
				UnitID:       unitID,
				PartyID:      a.claim.PartyID,
				ContextID:    a.unit.ContextID,
				DefinitionID: def.ID,
				Version:      def.Version,
				CreatedAt:    now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ingestResult{}, err
	}
	result.Batch = batch
	return result, nil
}

// dedupeKey is a unit's identity, derived from the work it describes rather
// than from when it was written.
//
// Everything that makes two rows the same piece of work is in it, and nothing
// else: the context and definition it was measured under, the activity, who it
// joins to, when it happened and how much of it there was. A source's own
// record reference is included when it supplies one, because a source that
// numbers its rows is telling us which are distinct and we should believe it.
//
// Deliberately excluded: the batch, the adapter, the transport and the
// timestamp of ingestion. Two identical rows submitted an hour apart through
// different transports are the same work, and paying twice for them is the
// failure this exists to prevent.
//
// The trade this makes is worth stating: a worker who genuinely does the same
// thing twice in one period, with the same outcome and no distinguishing field,
// is recorded once. That is a real cost, and it falls on the worker. It is also
// why sources should send a record reference — with one, the collision cannot
// happen, and the queue of definitions that need one is a shorter conversation
// than a queue of duplicate payments.
func dedupeKey(unit schema.Unit, record schema.CanonicalWorkEvidenceRecord) string {
	joining := record.WorkerJoiningIdentifier.Value
	if record.WorkerJoiningIdentifier.Kind == schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindNationalID {
		// Never the raw number, here or anywhere else.
		joining = hashNationalID(joining)
	}
	end := ""
	if unit.Period.End != nil {
		end = unit.Period.End.UTC().Format(time.RFC3339)
	}
	ref := ""
	if record.Provenance.SourceRecordRef != nil {
		ref = *record.Provenance.SourceRecordRef
	}
	parts := []string{
		unit.ContextID,
		unit.Definition.ID,
		fmt.Sprint(unit.Definition.Version),
		record.Activity,
		string(record.WorkerJoiningIdentifier.Kind),
		joining,
		unit.Period.Start.UTC().Format(time.RFC3339),
		end,
		fmt.Sprintf("%v %s", unit.Outcome.Value, unit.Outcome.Unit),
		ref,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// redact removes the raw national identifier before a record is stored.
//
// The unclear queue keeps the parsed record so a row can be re-attributed once
// the person is identified, rather than asked for again. That is right — and it
// is also the one place a raw national identifier could come to rest, because
// an unmatched row is exactly the row whose identifier nobody has resolved yet.
//
// The rule is absolute: a pairwise subject reference and a salted hash, nothing
// else. So the hash goes in the queue, the raw number does not, and
// re-attribution works from the hash because that is what the registry matches
// on anyway.
//
// This was found by an adversarial review, not by a test. The unmatched-row
// fixtures all joined on a phone number, so the national-identifier path
// through the queue was never exercised.
func redact(rec schema.CanonicalWorkEvidenceRecord) schema.CanonicalWorkEvidenceRecord {
	if rec.WorkerJoiningIdentifier.Kind == schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindNationalID {
		rec.WorkerJoiningIdentifier.Value = hashNationalID(rec.WorkerJoiningIdentifier.Value)
	}
	return rec
}

// windowRequest is what evidence tells confirmation. Deliberately the facts
// confirmation needs and nothing more — no outcome, no provenance. Confirmation
// asks a worker whether a record is true; it does not need to be able to
// re-derive the record.
type windowRequest struct {
	ClaimID      string    `json:"claimId"`
	UnitID       string    `json:"unitId"`
	PartyID      string    `json:"partyId"`
	ContextID    string    `json:"contextId"`
	DefinitionID string    `json:"definitionId"`
	Version      int       `json:"definitionVersion"`
	CreatedAt    time.Time `json:"createdAt"`
}

// consider decides one row's fate, returning a reason when it cannot become a
// claim. It writes nothing.
func (in *ingestor) consider(ctx context.Context, row adapters.Row, def schema.Definition,
	p ingestParams, now time.Time) (reason string, unit schema.Unit, claim schema.Claim) {
	if err := schema.Validate(schema.IDEvidenceRecord, row.Record); err != nil {
		return "the record does not satisfy the canonical evidence contract: " + err.Error(), unit, claim
	}
	if row.Record.Activity != def.Activity.Code {
		return fmt.Sprintf("activity %q is not what %s@%d defines (%q)",
			row.Record.Activity, def.ID, def.Version, def.Activity.Code), unit, claim
	}
	if row.Record.Outcome.Unit != def.OutcomeUnit {
		return fmt.Sprintf("outcome is counted in %q but the definition counts %q",
			row.Record.Outcome.Unit, def.OutcomeUnit), unit, claim
	}

	match, err := in.resolveWorker(ctx, row.Record.WorkerJoiningIdentifier, p.ContextID)
	if err != nil {
		return err.Error(), unit, claim
	}

	// §9 defines enrolment consent as the right to fetch and hold evidence
	// about this worker. A worker who has withdrawn it has asked us to stop,
	// and a withdrawal that does not stop anything is a checkbox.
	//
	// Only WITHDRAWN refuses. NONE does not, and that asymmetry is deliberate:
	// a deployment that has not yet captured consent for its existing roster
	// would otherwise stop recording work for everyone the day this shipped,
	// and a worker whose evidence silently stopped counting because of a
	// migration gap is exactly the harm this is supposed to prevent. Closing
	// that gap is a deployment's own job and #24 says so.
	//
	// Work already recorded is untouched. Consent governs what may be
	// collected next; taking away the record of work somebody did because they
	// later withdrew would make withdrawal cost them their history.
	if match.EnrolmentConsent == "WITHDRAWN" {
		return "this worker has withdrawn enrolment consent, so no new evidence about " +
			"them is recorded (§9). Work already recorded is unaffected", unit, claim
	}

	unit = schema.Unit{
		ID:         id.New(in.clock, "unit"),
		Definition: schema.VersionedRef{ID: def.ID, Version: def.Version},
		ContextID:  p.ContextID,
		Outcome:    row.Record.Outcome,
		Period:     row.Record.Period,
		Geography:  row.Record.Geography,
		Enrichment: row.Record.Enrichment,
		Provenance: row.Record.Provenance,
		CreatedAt:  now,
	}
	claim = schema.Claim{
		ID:      id.New(in.clock, "claim"),
		UnitID:  unit.ID,
		PartyID: match.PartyID,
		State:   schema.ClaimStateDRAFT,
		Matched: &schema.ClaimMatched{
			Key:        schema.ClaimMatchedKey(match.Key),
			Confidence: match.Confidence,
		},
		CreatedAt: now,
	}
	return "", unit, claim
}

// resolveWorker asks the registry. The raw identifier goes over the wire and is
// never written down here: the registry matches on a salted hash, and this
// service has no column to put a national identifier in (W9).
func (in *ingestor) resolveWorker(ctx context.Context,
	joining schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifier, contextID string) (match, error) {
	kind := ""
	value := joining.Value
	switch joining.Kind {
	case schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindNationalID:
		kind = "national-id-hash"
		value = hashNationalID(joining.Value)
	case schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindPhone:
		kind = "contact-route"
	case schema.CanonicalWorkEvidenceRecordWorkerJoiningIdentifierKindRosterID:
		kind = "roster-id"
	}

	var m match
	err := in.registry.Get(ctx, fmt.Sprintf("/v1/resolve?kind=%s&value=%s&contextId=%s",
		kind, urlSafe(value), urlSafe(contextID)), &m)
	switch client.Code(err) {
	case 0:
		return m, nil
	case http.StatusNotFound:
		return match{}, fmt.Errorf("no party matches the %s this row joins on", joining.Kind)
	case http.StatusConflict:
		// The registry recorded a hold. This row waits for a person to say
		// which candidate it is — it never picks one (W7).
		return match{}, fmt.Errorf("more than one party carries this identifier; " +
			"the registry is holding the match rather than merging")
	default:
		return match{}, fmt.Errorf("registry could not resolve the worker: %w", err)
	}
}

type match struct {
	PartyID          string  `json:"partyId"`
	Key              string  `json:"key"`
	Confidence       float64 `json:"confidence"`
	EnrolmentConsent string  `json:"enrolmentConsent"`
}

func (in *ingestor) permits(ctx context.Context, p ingestParams) (bool, error) {
	var out struct {
		Permitted bool `json:"permitted"`
	}
	err := in.registry.Get(ctx, fmt.Sprintf(
		"/v1/authorizations/permits?partyId=%s&function=submit-work-evidence&contextId=%s",
		urlSafe(p.SubmittedBy), urlSafe(p.ContextID)), &out)
	if err != nil {
		return false, fmt.Errorf("registry could not check the authorization: %w", err)
	}
	return out.Permitted, nil
}

func (in *ingestor) definition(ctx context.Context, defID string) (schema.Definition, error) {
	var def schema.Definition
	if err := in.definitions.Get(ctx, "/v1/definitions/"+urlSafe(defID), &def); err != nil {
		return def, fmt.Errorf("definitions service could not resolve %s: %w", defID, err)
	}
	return def, nil
}
