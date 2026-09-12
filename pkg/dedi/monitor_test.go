package dedi

import (
	"context"
	"errors"
	"testing"
)

// memStore is an in-memory CheckpointStore for the monitor tests.
type memStore struct {
	cp Checkpoint
	ok bool
}

func (m *memStore) Load(_ context.Context) (Checkpoint, bool, error) { return m.cp, m.ok, nil }
func (m *memStore) Save(_ context.Context, cp Checkpoint) error {
	m.cp, m.ok = cp, true
	return nil
}

// The first run has no pin: the monitor adopts the current checkpoint as the
// baseline and records it.
func TestMonitorFirstObservationPins(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	srv := log.serve()
	defer srv.Close()

	store := &memStore{}
	m := NewMonitor(nodeFor(t, srv.URL, verifier), store, "", nil)

	res, err := m.Check(context.Background())
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if !res.FirstObservation || !res.Consistent {
		t.Fatalf("want first observation + consistent, got %+v", res)
	}
	if !store.ok || store.cp.Size != 2 {
		t.Fatalf("baseline not pinned: %+v", store)
	}
}

// A subsequent append is accepted and advances the pin.
func TestMonitorAdvancesOnAppend(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	store := &memStore{}
	m := NewMonitor(nodeFor(t, srv.URL, verifier), store, "", nil)
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatalf("first check: %v", err)
	}

	log.append("b")
	log.append("c")
	res, err := m.Check(context.Background())
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if res.FirstObservation || !res.Consistent {
		t.Fatalf("want a consistent advance, got %+v", res)
	}
	if store.cp.Size != 3 {
		t.Fatalf("pin advanced to %d, want 3", store.cp.Size)
	}
}

// The alarm path: after a pin is established, the operator rewrites history. The
// monitor returns Rewritten with an error AND leaves the pin untouched, so the
// pre-rewrite baseline is preserved.
func TestMonitorAlarmsAndKeepsBaselineOnRewrite(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	log.append("b")
	log.append("c")
	srv := log.serve()
	defer srv.Close()

	store := &memStore{}
	m := NewMonitor(nodeFor(t, srv.URL, verifier), store, "", nil)
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatalf("first check: %v", err)
	}
	pinnedBefore := store.cp

	log.rewrite(1, "b-tampered")
	res, err := m.Check(context.Background())
	if !errors.Is(err, ErrHistoryRewritten) {
		t.Fatalf("want ErrHistoryRewritten, got %v", err)
	}
	if !res.Rewritten {
		t.Fatalf("want Rewritten result, got %+v", res)
	}
	if store.cp.Root != pinnedBefore.Root || store.cp.Size != pinnedBefore.Size {
		t.Fatal("pin must not advance onto a rewritten checkpoint")
	}
}

// A witness that confirms consistency is reported as such.
func TestMonitorWitnessConfirms(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	// A stand-in witness node that vouches for the target's consistency.
	wsrv := serveWitness(t, map[string]witnessData{
		"crest.test/dedi": {witnessed: true, consistencyOK: true, size: 1},
	})
	defer wsrv.Close()

	store := &memStore{}
	m := NewMonitor(nodeFor(t, srv.URL, verifier), store, wsrv.URL, nil)
	res, err := m.Check(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !res.WitnessChecked || !res.WitnessConsistencyOK {
		t.Fatalf("want witness confirmation, got %+v", res)
	}
}

// A witness that has not confirmed the log is reported (a warning), but the
// node's own consistency proof still holds, so the check does not fail.
func TestMonitorWitnessUnconfirmedDoesNotFail(t *testing.T) {
	log, verifier := newFakeLog(t, "crest.test/dedi")
	log.append("a")
	srv := log.serve()
	defer srv.Close()

	wsrv := serveWitness(t, map[string]witnessData{
		"crest.test/dedi": {witnessed: false},
	})
	defer wsrv.Close()

	store := &memStore{}
	m := NewMonitor(nodeFor(t, srv.URL, verifier), store, wsrv.URL, nil)
	res, err := m.Check(context.Background())
	if err != nil {
		t.Fatalf("check must not fail on an unconfirmed witness: %v", err)
	}
	if !res.Consistent || res.WitnessConsistencyOK {
		t.Fatalf("want consistent-but-unwitnessed, got %+v", res)
	}
}
