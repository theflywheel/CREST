package evidence

import (
	"context"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// w6_3, "What the project received, and where it sits" (§8).
//
// This is a read composing three tables that already exist: the batch is the
// receipt for one submission (Batch's own doc comment), a unit is what
// happened, and a claim is who it is attributed to. Nothing new is stored —
// the "where it sits" half of the title is answered from the unclear queue's
// own ordering (openUnclear, §4), because that queue is the only place CREST
// keeps a position for anything.
//
// w6_1 and w6_2 (the scoped request and the file-back answer) are marked
// illustrative in docs/journey-traceability.json: no scoped-link or upload
// endpoint exists yet, so there is no per-external-contact receipt to read.
// This endpoint answers the question the frame is actually asking — a
// project-side view of what arrived through the one ingestion door that does
// exist, POST /v1/batches — by batch id.

// unitReceipt is what arrived, for one unit in a batch, with how it was
// attributed.
type unitReceipt struct {
	UnitID     string              `json:"unitId"`
	Definition schema.VersionedRef `json:"definition"`
	Outcome    schema.Outcome      `json:"outcome"`
	CreatedAt  time.Time           `json:"createdAt"`
	Claims     []claimReceipt      `json:"claims"`
}

// claimReceipt is the ingestion state of one attribution.
type claimReceipt struct {
	ClaimID string            `json:"claimId"`
	PartyID string            `json:"partyId"`
	State   schema.ClaimState `json:"state"`
}

// unclearReceipt is a row nobody could attribute, with its place in the queue
// if it is still open.
type unclearReceipt struct {
	ID            string     `json:"id"`
	RowRef        string     `json:"rowRef"`
	Kind          string     `json:"kind"`
	Reason        string     `json:"reason"`
	CreatedAt     time.Time  `json:"createdAt"`
	ResolvedAt    *time.Time `json:"resolvedAt,omitempty"`
	QueuePosition *int       `json:"queuePosition,omitempty"`
}

// batchReceipt composes the pieces into what w6_3 shows: what arrived, when,
// its ingestion state, and where it sits. Taking already-fetched slices
// (rather than a DB handle) is what lets the composition be tested without
// one.
func batchReceipt(b Batch, units []unitReceipt, unclear []UnclearRow, openOrder []string) map[string]any {
	position := make(map[string]int, len(openOrder))
	for i, id := range openOrder {
		position[id] = i + 1 // 1-based: "third in the queue" is what a person reads
	}
	rows := make([]unclearReceipt, len(unclear))
	for i, u := range unclear {
		row := unclearReceipt{
			ID: u.ID, RowRef: u.RowRef, Kind: u.Kind, Reason: u.Reason,
			CreatedAt: u.CreatedAt,
		}
		if pos, open := position[u.ID]; open {
			p := pos
			row.QueuePosition = &p
		}
		rows[i] = row
	}
	return map[string]any{
		"batch":   b,
		"units":   units,
		"unclear": rows,
	}
}

// unitsWithClaims reads every unit in a batch and its claims, in the shape
// batchReceipt needs.
func unitsWithClaims(ctx context.Context, q store.Querier, batchID string) ([]unitReceipt, error) {
	rows, err := q.Query(ctx, `
		SELECT id, doc FROM units WHERE batch_id = $1 ORDER BY created_at, id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		id  string
		doc []byte
	}
	fetched, err := store.Collect(rows, func(r store.Row) (row, error) {
		var out row
		return out, r.Scan(&out.id, &out.doc)
	})
	if err != nil {
		return nil, err
	}
	out := make([]unitReceipt, 0, len(fetched))
	for _, f := range fetched {
		u, err := getUnit(ctx, q, f.id)
		if err != nil {
			return nil, err
		}
		claims, err := unitClaims(ctx, q, f.id)
		if err != nil {
			return nil, err
		}
		out = append(out, unitReceipt{
			UnitID: u.ID, Definition: u.Definition, Outcome: u.Outcome,
			CreatedAt: u.CreatedAt, Claims: claims,
		})
	}
	return out, nil
}

func unitClaims(ctx context.Context, q store.Querier, unitID string) ([]claimReceipt, error) {
	rows, err := q.Query(ctx, `
		SELECT id, party_id, state FROM claims WHERE unit_id = $1 ORDER BY id`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (claimReceipt, error) {
		var c claimReceipt
		var state string
		if err := r.Scan(&c.ClaimID, &c.PartyID, &state); err != nil {
			return claimReceipt{}, err
		}
		c.State = schema.ClaimState(state)
		return c, nil
	})
}

// unclearForBatch reads every unclear row a batch produced, resolved or not —
// unlike openUnclear, which is the working queue and only shows open ones.
func unclearForBatch(ctx context.Context, q store.Querier, batchID string) ([]UnclearRow, error) {
	rows, err := q.Query(ctx, `
		SELECT id, batch_id, row_ref, kind, reason, record, created_at
		FROM unclear_rows WHERE batch_id = $1 ORDER BY created_at`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (UnclearRow, error) {
		var u UnclearRow
		return u, r.Scan(&u.ID, &u.BatchID, &u.RowRef, &u.Kind, &u.Reason, &u.Record, &u.CreatedAt)
	})
}

// openUnclearIDs is the global open-queue order, id-only — batchReceipt only
// needs position, never another batch's row content.
func openUnclearIDs(ctx context.Context, q store.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM unclear_rows WHERE resolved_at IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
}

// getBatchReceipt is w6_3's endpoint: GET /v1/batches/{id}/receipt.
func (h *handlers) getBatchReceipt(w http.ResponseWriter, r *http.Request) {
	if !identity.Authenticated(w, r, h.d.Log, h.d.Authenticating) {
		return
	}
	batchID := r.PathValue("id")
	b, err := getBatch(r.Context(), h.d.DB.Q(), batchID)
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "batch", err, store.ErrNotFound)
		return
	}
	units, err := unitsWithClaims(r.Context(), h.d.DB.Q(), batchID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read batch units", err)
		return
	}
	unclear, err := unclearForBatch(r.Context(), h.d.DB.Q(), batchID)
	if err != nil {
		httpx.Fail(w, h.d.Log, "read batch unclear rows", err)
		return
	}
	openOrder, err := openUnclearIDs(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "read open unclear queue", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, batchReceipt(b, units, unclear, openOrder))
}
