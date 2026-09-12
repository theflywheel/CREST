package dedi

import (
	"context"
	"os"
	"testing"
)

// A live check against a real DeDi node, gated on DEDI_TEST_URL so CI without a
// node skips it. It exists to catch wire-contract drift between this second
// implementation and the node's own — the endpoint paths, the checkpoint note
// layout, and the consistency-proof response shape — which unit tests against a
// fake cannot catch because the fake is written to the same assumptions.
//
//	DEDI_TEST_URL=http://127.0.0.1:58481 \
//	  go test ./pkg/dedi/ -run TestLiveNodeCheckpointContract -v
func TestLiveNodeCheckpointContract(t *testing.T) {
	base := os.Getenv("DEDI_TEST_URL")
	if base == "" {
		t.Skip("requires DEDI_TEST_URL pointing at a real DeDi node")
	}
	n, err := NewReadNode(base)
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	ctx := context.Background()

	cp, err := n.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("fetch/parse real checkpoint: %v", err)
	}
	if cp.Origin == "" || cp.Size < 0 {
		t.Fatalf("checkpoint did not parse: %+v", cp)
	}
	t.Logf("live checkpoint: origin=%q size=%d", cp.Origin, cp.Size)

	// Pinning the current checkpoint and immediately re-confirming exercises the
	// same-size branch against a real root; growth would exercise a real
	// consistency proof, which the endpoint we just parsed produces.
	if _, err := n.ConfirmExtends(ctx, cp); err != nil {
		t.Fatalf("ConfirmExtends against the node's own current checkpoint: %v", err)
	}

	// If the node has any history, a consistency proof to the current size must
	// verify against the current root — the real wire, checked by the real tlog.
	if cp.Size >= 2 {
		if _, err := n.consistencyProof(ctx, 1, cp.Size); err != nil {
			t.Fatalf("fetch real consistency proof 1→%d: %v", cp.Size, err)
		}
	}
}
