package dedi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// The consumer that turns the checkpoint primitives into an ongoing guarantee
// (#241). A Monitor holds the newest checkpoint CREST has authenticated, and on
// each check refuses any checkpoint that is not a forward-only extension of it.
// The pin is persisted by the caller (a CheckpointStore) so the guarantee
// survives a restart: a rewrite that happens while CREST is down is still caught
// on the next boot, because the pin from before the downtime is compared against
// what the node serves after it.
//
// A monitor never advances its pin past a checkpoint it could not authenticate
// or could not prove consistent. That is the whole discipline: the pin is the
// last point CREST is certain the log had not been rewritten, and moving it
// forward on a bad checkpoint would launder the rewrite into the new baseline.

// CheckpointStore persists the last checkpoint a Monitor verified. Load returns
// ok=false when nothing has been pinned yet (the first-ever run).
type CheckpointStore interface {
	Load(ctx context.Context) (cp Checkpoint, ok bool, err error)
	Save(ctx context.Context, cp Checkpoint) error
}

// Checkpointer is the slice of *Node a Monitor needs. An interface so the
// monitor is testable without an HTTP server and so a caller cannot hand it a
// fallback publisher by mistake.
type Checkpointer interface {
	ConfirmExtends(ctx context.Context, pinned Checkpoint) (Checkpoint, error)
	WitnessVerdictFor(ctx context.Context, witnessBaseURL, targetOrigin string) (WitnessVerdict, error)
}

// Monitor watches one DeDi log for history rewrites and, where a witness ring
// exists, confirms an independent witness agrees.
type Monitor struct {
	node       Checkpointer
	store      CheckpointStore
	witnessURL string // an independent witness node, empty until the ring has one (#76)
	log        *slog.Logger
}

// NewMonitor builds a Monitor. witnessURL may be empty; see WitnessURL on
// Config for why that is an honest state rather than a misconfiguration.
func NewMonitor(node Checkpointer, store CheckpointStore, witnessURL string, log *slog.Logger) *Monitor {
	return &Monitor{node: node, store: store, witnessURL: witnessURL, log: log}
}

// Result is what one check concluded. It is a report, not a verdict on a single
// credential: it says whether the log as a whole has stayed append-only.
type Result struct {
	// Size and Root are the checkpoint the check ended on.
	Size int64
	Root string
	// Authenticated reports the checkpoint signature checked out against the
	// configured node key.
	Authenticated bool
	// Consistent reports the current checkpoint is a forward-only extension of
	// the pinned one (or that this was the first observation).
	Consistent bool
	// Rewritten is the alarm: the node served a checkpoint that cannot be an
	// append-only extension of what CREST pinned. When true the pin is NOT
	// advanced.
	Rewritten bool
	// FirstObservation reports there was no prior pin; the current checkpoint
	// was adopted as the baseline.
	FirstObservation bool
	// WitnessChecked reports an independent witness was configured and asked.
	WitnessChecked bool
	// WitnessConsistencyOK is that witness's verdict, meaningful only when
	// WitnessChecked is true.
	WitnessConsistencyOK bool
	// Detail carries the human-readable reason on a rewrite or a witness alarm.
	Detail string
}

// Check runs one consistency check and, on success, advances the pin. It
// returns a Result describing what it found. A rewrite is returned as a Result
// with Rewritten=true AND a non-nil error, so a caller that ignores the Result
// still cannot ignore the alarm.
func (m *Monitor) Check(ctx context.Context) (Result, error) {
	pinned, ok, err := m.store.Load(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load pinned checkpoint: %w", err)
	}
	if !ok {
		pinned = Checkpoint{} // Size 0: first observation, nothing to contradict.
	}

	cur, err := m.node.ConfirmExtends(ctx, pinned)
	if errors.Is(err, ErrHistoryRewritten) {
		res := Result{Size: cur.Size, Root: fmt.Sprintf("%x", cur.Root),
			Authenticated: cur.Verified, Rewritten: true, Detail: err.Error()}
		// The one line in this system that must never be swallowed: the log a
		// verifier trusts was rewritten. Logged at Error, and the pin is left
		// where it was so a later honest checkpoint is still measured against
		// the pre-rewrite baseline rather than the tampered one.
		if m.log != nil {
			m.log.Error("DeDi log history rewrite detected", "detail", err.Error(),
				"pinnedSize", pinned.Size, "servedSize", cur.Size)
		}
		return res, err
	}
	if err != nil {
		// A transport failure or an unauthenticated checkpoint. Not an alarm on
		// its own — but the pin is not advanced, so a genuine rewrite hiding
		// behind a signature failure is not adopted as the new baseline.
		return Result{}, err
	}

	res := Result{
		Size: cur.Size, Root: fmt.Sprintf("%x", cur.Root),
		Authenticated: cur.Verified, Consistent: true,
		FirstObservation: !ok,
	}
	if err := m.store.Save(ctx, cur); err != nil {
		return res, fmt.Errorf("advance pinned checkpoint: %w", err)
	}

	// An independent witness, where one exists, turns "this node vouches for its
	// own history" into "a party other than this node vouches for it". Its
	// failure is reported but does not fail the check: the node's own
	// consistency proof already held, and a flaky witness must not read as a
	// rewrite.
	if m.witnessURL != "" && cur.Origin != "" {
		res.WitnessChecked = true
		v, werr := m.node.WitnessVerdictFor(ctx, m.witnessURL, cur.Origin)
		switch {
		case werr != nil:
			res.Detail = "independent witness could not be reached: " + werr.Error()
			if m.log != nil {
				m.log.Warn("DeDi witness verdict unavailable", "error", werr.Error())
			}
		case !v.Witnessed || !v.ConsistencyOK:
			res.WitnessConsistencyOK = false
			res.Detail = "independent witness has not confirmed this log's consistency: " + v.Detail
			if m.log != nil {
				m.log.Warn("DeDi log not confirmed by independent witness",
					"witnessed", v.Witnessed, "consistencyOK", v.ConsistencyOK, "detail", v.Detail)
			}
		default:
			res.WitnessConsistencyOK = true
		}
	}
	return res, nil
}
