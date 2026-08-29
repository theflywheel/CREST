package evidence

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
//
// Kind is what makes the row workable. Reason is written for a person; Kind is
// written for the code that decides whether this row can ever become a claim,
// and only unclearUnattributed can (0005).
type UnclearRow struct {
	ID        string          `json:"id"`
	BatchID   string          `json:"batchId"`
	RowRef    string          `json:"rowRef"`
	Kind      string          `json:"kind"`
	Reason    string          `json:"reason"`
	Record    json.RawMessage `json:"record,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// The kinds of unclear row. Only the first can be re-attributed to a worker;
// 0005's comment says why for each of the others.
const (
	unclearUnattributed = "unattributed"
	unclearContract     = "contract"
	unclearRejected     = "rejected"
	unclearWithdrawn    = "consent-withdrawn"
)

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
		INSERT INTO unclear_rows (id, batch_id, row_ref, kind, reason, record, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		u.ID, u.BatchID, u.RowRef, u.Kind, u.Reason, u.Record, u.CreatedAt)
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

// listClaims returns a worker's claims, across any merge (#100).
//
// partyIDs rather than one id: a party absorbed into another still owns every
// claim recorded before the merge, and those claims still name it, because a
// claim that said whose work it was at the time is a true statement and is not
// rewritten. Filtering on one id would leave the survivor's history with a
// hole exactly where the system corrected itself about who they were.
//
// An empty slice means no filter, which is the same as before merges existed.
func listClaims(ctx context.Context, q store.Querier, partyIDs []string, state string) ([]schema.Claim, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM claims
		-- COALESCE because a nil slice arrives as a NULL array, not an empty
		-- one, and cardinality(NULL) is NULL rather than 0 — which makes the
		-- whole predicate NULL and the unfiltered list silently empty.
		WHERE (COALESCE(cardinality($1::text[]), 0) = 0 OR party_id = ANY($1))
		  AND ($2 = '' OR state = $2)
		ORDER BY id`, partyIDs, state)
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
		SELECT id, batch_id, row_ref, kind, reason, record, created_at
		FROM unclear_rows WHERE resolved_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (UnclearRow, error) {
		var u UnclearRow
		return u, r.Scan(&u.ID, &u.BatchID, &u.RowRef, &u.Kind, &u.Reason, &u.Record, &u.CreatedAt)
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

// getOpenUnclear reads one unresolved row, locking it for the transaction that
// is about to resolve it.
//
// FOR UPDATE rather than a plain read: two people working the same queue is the
// normal case, not the exotic one, and without the lock both can pass the
// "still open" check and both create a claim. The claim table's uniqueness on
// (unit_id, party_id) would catch two resolutions to the same worker, but not
// two resolutions to different workers — which is the same work paid twice.
func getOpenUnclear(ctx context.Context, tx store.Querier, rowID string) (UnclearRow, error) {
	var u UnclearRow
	err := tx.QueryRow(ctx, `
		SELECT id, batch_id, row_ref, kind, reason, record, created_at
		FROM unclear_rows WHERE id = $1 AND resolved_at IS NULL FOR UPDATE`, rowID).
		Scan(&u.ID, &u.BatchID, &u.RowRef, &u.Kind, &u.Reason, &u.Record, &u.CreatedAt)
	return u, err
}

// markUnclearResolved closes the row, naming who resolved it, to whom, and
// which claim now carries the work.
func markUnclearResolved(ctx context.Context, tx store.Querier,
	rowID, partyID, resolvedBy, claimID string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE unclear_rows
		   SET resolved_at = $2, resolved_to = $3, resolved_by = $4, resolution_claim_id = $5
		 WHERE id = $1 AND resolved_at IS NULL`,
		rowID, at, partyID, resolvedBy, claimID)
	return err
}
