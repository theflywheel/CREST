package evidence

import (
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
)

// w6_3: the receipt composes what arrived with where it sits — an open
// unclear row carries a queue position, a resolved one does not, and a batch
// with nothing unclear composes cleanly.

func TestBatchReceiptMarksQueuePositionOnlyForOpenRows(t *testing.T) {
	b := Batch{ID: "batch-1", RowsTotal: 3, RowsAccepted: 2, RowsUnclear: 1}
	unclear := []UnclearRow{
		{ID: "u1", BatchID: "batch-1", RowRef: "row-7", Kind: unclearUnattributed, Reason: "no joining identifier"},
	}
	openOrder := []string{"u-other", "u1"} // u1 is second in the global open queue
	out := batchReceipt(b, nil, unclear, openOrder)
	rows := out["unclear"].([]unclearReceipt)
	if len(rows) != 1 {
		t.Fatalf("expected one unclear row, got %+v", rows)
	}
	if rows[0].QueuePosition == nil || *rows[0].QueuePosition != 2 {
		t.Fatalf("expected queue position 2 (1-based), got %+v", rows[0].QueuePosition)
	}
}

func TestBatchReceiptLeavesAResolvedRowWithNoQueuePosition(t *testing.T) {
	b := Batch{ID: "batch-1"}
	unclear := []UnclearRow{{ID: "u1", BatchID: "batch-1", Kind: unclearRejected, Reason: "bad definition"}}
	// u1 no longer appears in the open order: it was resolved.
	out := batchReceipt(b, nil, unclear, []string{"u-other"})
	rows := out["unclear"].([]unclearReceipt)
	if rows[0].QueuePosition != nil {
		t.Fatalf("a resolved row must carry no queue position, got %v", *rows[0].QueuePosition)
	}
}

func TestBatchReceiptComposesCleanlyWithNothingUnclear(t *testing.T) {
	b := Batch{ID: "batch-1"}
	units := []unitReceipt{
		{
			UnitID:     "unit-1",
			Definition: schema.VersionedRef{ID: "bednet-distribution", Version: 1},
			Outcome:    schema.Outcome{Value: 12, Unit: "bednets-distributed"},
			CreatedAt:  time.Now(),
			Claims: []claimReceipt{
				{ClaimID: "claim-1", PartyID: "party-1", State: schema.ClaimStateACCEPTED},
			},
		},
	}
	out := batchReceipt(b, units, nil, nil)
	gotUnits := out["units"].([]unitReceipt)
	if len(gotUnits) != 1 || gotUnits[0].Claims[0].State != schema.ClaimStateACCEPTED {
		t.Fatalf("unexpected units in receipt: %+v", gotUnits)
	}
	rows := out["unclear"].([]unclearReceipt)
	if len(rows) != 0 {
		t.Fatalf("expected no unclear rows, got %+v", rows)
	}
}
