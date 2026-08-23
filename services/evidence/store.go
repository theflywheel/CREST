package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

const topicClaimCreated = "claim.created"

// topicSourceQuiet tells a source's owner that it has stopped sending.
//
// A side effect that must not be lost, for a reason worth stating: this is the
// only alert in the system nobody else will raise. A missed payment is noticed
// by the worker who did not get paid; a rejected row is in the unclear queue; a
// wrong record gets disputed. A silent source produces no artefact anywhere,
// so if this message is dropped the outage is simply invisible.
const topicSourceQuiet = "source.quiet"

// Batch is the receipt for one submission. Every count is here so that "we sent
// 500 rows and 12 people were not paid" is a question with an answer.
type Batch struct {
	ID                string    `json:"id"`
	ContextID         string    `json:"contextId"`
	DefinitionID      string    `json:"definitionId"`
	DefinitionVersion int       `json:"definitionVersion"`
	SubmittedBy       string    `json:"submittedBy"`
	AdapterRef        string    `json:"adapterRef"`
	RowsTotal         int       `json:"rowsTotal"`
	RowsAccepted      int       `json:"rowsAccepted"`
	RowsUnclear       int       `json:"rowsUnclear"`
	CreatedAt         time.Time `json:"createdAt"`
}

// UnclearRow is a row that describes work nobody could attribute.
type UnclearRow struct {
	ID        string          `json:"id"`
	BatchID   string          `json:"batchId"`
	RowRef    string          `json:"rowRef"`
	Reason    string          `json:"reason"`
	Record    json.RawMessage `json:"record,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func insertBatch(ctx context.Context, tx store.Querier, b Batch) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO batches (id, context_id, definition_id, definition_version, submitted_by,
		                     adapter_ref, rows_total, rows_accepted, rows_unclear, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		b.ID, b.ContextID, b.DefinitionID, b.DefinitionVersion, b.SubmittedBy,
		b.AdapterRef, b.RowsTotal, b.RowsAccepted, b.RowsUnclear, b.CreatedAt)
	return err
}

// insertUnit stores a unit, or returns the id of the one already describing the
// same work.
//
// Converging rather than inserting is what makes a re-submitted batch harmless.
// The returned id is then what the claim is written against, so the claim's own
// (unit_id, party_id) uniqueness catches the duplicate and nobody is paid twice.
func insertUnit(ctx context.Context, tx store.Querier, batchID string, u schema.Unit, dedupeKey string) (string, error) {
	doc, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	var existingID string
	err = tx.QueryRow(ctx, `
		INSERT INTO units (id, batch_id, context_id, definition_id, definition_version,
		                   doc, created_at, dedupe_key)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (dedupe_key) DO UPDATE SET dedupe_key = units.dedupe_key
		RETURNING id`,
		u.ID, batchID, u.ContextID, u.Definition.ID, u.Definition.Version,
		doc, u.CreatedAt, dedupeKey).Scan(&existingID)
	return existingID, err
}

// insertClaim is idempotent on (unit, party). Re-running a batch is an ordinary
// operational event; paying someone twice for it is not.
func insertClaim(ctx context.Context, tx store.Querier, c schema.Claim) (bool, error) {
	doc, err := json.Marshal(c)
	if err != nil {
		return false, err
	}
	affected, err := tx.Exec(ctx, `
		INSERT INTO claims (id, unit_id, party_id, state, doc, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (unit_id, party_id) DO NOTHING`,
		c.ID, c.UnitID, c.PartyID, string(c.State), doc, c.CreatedAt)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func insertUnclear(ctx context.Context, tx store.Querier, u UnclearRow) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO unclear_rows (id, batch_id, row_ref, reason, record, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.BatchID, u.RowRef, u.Reason, u.Record, u.CreatedAt)
	return err
}

func getUnit(ctx context.Context, q store.Querier, unitID string) (schema.Unit, error) {
	var doc []byte
	err := q.QueryRow(ctx, `SELECT doc FROM units WHERE id = $1`, unitID).Scan(&doc)
	if err != nil {
		return schema.Unit{}, err
	}
	var u schema.Unit
	return u, json.Unmarshal(doc, &u)
}

func getClaim(ctx context.Context, q store.Querier, claimID string) (schema.Claim, error) {
	var doc []byte
	err := q.QueryRow(ctx, `SELECT doc FROM claims WHERE id = $1`, claimID).Scan(&doc)
	if err != nil {
		return schema.Claim{}, err
	}
	var c schema.Claim
	return c, json.Unmarshal(doc, &c)
}

func listClaims(ctx context.Context, q store.Querier, partyID, state string) ([]schema.Claim, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM claims
		WHERE ($1 = '' OR party_id = $1) AND ($2 = '' OR state = $2)
		ORDER BY id`, partyID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Claim, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.Claim{}, err
		}
		var c schema.Claim
		return c, json.Unmarshal(doc, &c)
	})
}

func openUnclear(ctx context.Context, q store.Querier) ([]UnclearRow, error) {
	rows, err := q.Query(ctx, `
		SELECT id, batch_id, row_ref, reason, record, created_at
		FROM unclear_rows WHERE resolved_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (UnclearRow, error) {
		var u UnclearRow
		return u, r.Scan(&u.ID, &u.BatchID, &u.RowRef, &u.Reason, &u.Record, &u.CreatedAt)
	})
}

func getBatch(ctx context.Context, q store.Querier, batchID string) (Batch, error) {
	var b Batch
	err := q.QueryRow(ctx, `
		SELECT id, context_id, definition_id, definition_version, submitted_by, adapter_ref,
		       rows_total, rows_accepted, rows_unclear, created_at
		FROM batches WHERE id = $1`, batchID).
		Scan(&b.ID, &b.ContextID, &b.DefinitionID, &b.DefinitionVersion, &b.SubmittedBy,
			&b.AdapterRef, &b.RowsTotal, &b.RowsAccepted, &b.RowsUnclear, &b.CreatedAt)
	return b, err
}
