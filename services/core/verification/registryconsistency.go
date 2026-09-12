package verification

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/mod/sumdb/note"

	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// Consuming DeDi's tamper-evidence in the verification service (#241).
//
// The trust chain hands a verifier inclusion-proof URLs, which answer "is this
// record in the log rooted at R?". They do not answer "was the log rewritten to
// produce R?" — the threat a transparency log exists to defeat. This is the
// consumer that closes that gap for CREST: it pins the newest checkpoint it has
// authenticated and, on boot and on demand, refuses any checkpoint that is not
// an append-only extension of the pin, alarming loudly when one appears. See
// pkg/dedi/checkpoint.go and pkg/dedi/monitor.go for the verification itself.

// pgCheckpointStore persists the pinned checkpoint in the singleton
// dedi_checkpoint_pin row (migration 0011).
type pgCheckpointStore struct{ q store.Querier }

func (s pgCheckpointStore) Load(ctx context.Context) (dedi.Checkpoint, bool, error) {
	var note []byte
	var verified bool
	err := s.q.QueryRow(ctx,
		`SELECT note, verified FROM dedi_checkpoint_pin WHERE id = true`).Scan(&note, &verified)
	if errors.Is(err, store.ErrNotFound) {
		return dedi.Checkpoint{}, false, nil
	}
	if err != nil {
		return dedi.Checkpoint{}, false, err
	}
	cp, err := dedi.ParseCheckpointNote(note)
	if err != nil {
		return dedi.Checkpoint{}, false, fmt.Errorf("stored checkpoint note is unreadable: %w", err)
	}
	cp.Verified = verified
	return cp, true, nil
}

func (s pgCheckpointStore) Save(ctx context.Context, cp dedi.Checkpoint) error {
	_, err := s.q.Exec(ctx, `
		INSERT INTO dedi_checkpoint_pin (id, origin, tree_size, root_hex, note, verified, pinned_at)
		VALUES (true, $1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET
			origin = EXCLUDED.origin, tree_size = EXCLUDED.tree_size,
			root_hex = EXCLUDED.root_hex, note = EXCLUDED.note,
			verified = EXCLUDED.verified, pinned_at = now()`,
		cp.Origin, cp.Size, fmt.Sprintf("%x", cp.Root), cp.Note, cp.Verified)
	return err
}

// consistencyMonitor builds the monitor from configuration, or returns nil when
// this deployment runs on the Postgres fallback (no log to monitor). The read
// node needs no publisher key — every call it makes is an unsigned read.
func consistencyMonitor(d service.Deps) (*dedi.Monitor, error) {
	cfg := dedi.LoadConfig()
	if cfg.URL == "" {
		return nil, nil
	}
	node, err := dedi.NewReadNode(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.CheckpointKey != "" {
		v, err := note.NewVerifier(cfg.CheckpointKey)
		if err != nil {
			return nil, fmt.Errorf("DEDI_CHECKPOINT_KEY unusable: %w", err)
		}
		node.SetCheckpointVerifier(v)
	}
	return dedi.NewMonitor(node, pgCheckpointStore{d.DB.Q()}, cfg.WitnessURL, d.Log), nil
}

// checkRegistryConsistencyAtBoot runs one check at start-up. A detected rewrite
// is logged at Error but is deliberately NOT fatal: refusing to start would take
// verification offline for every honest credential too, and the whole point of
// the alarm is that a human sees it and decides. A transport failure (the node
// is briefly unreachable at boot) is a warning, not an alarm.
func checkRegistryConsistencyAtBoot(d service.Deps) {
	m, err := consistencyMonitor(d)
	if err != nil {
		d.Log.Error("could not build the DeDi consistency monitor", "error", err)
		return
	}
	if m == nil {
		return // fallback mode; the trust chain already says facts carry no proof
	}
	res, err := m.Check(context.Background())
	switch {
	case errors.Is(err, dedi.ErrHistoryRewritten):
		d.Log.Error("DeDi log consistency check FAILED at boot — history may have been rewritten",
			"detail", res.Detail, "servedSize", res.Size)
	case err != nil:
		d.Log.Warn("DeDi log consistency could not be checked at boot", "error", err)
	default:
		d.Log.Info("DeDi log consistency confirmed at boot",
			"size", res.Size, "authenticated", res.Authenticated,
			"firstObservation", res.FirstObservation,
			"witnessChecked", res.WitnessChecked, "witnessConsistencyOK", res.WitnessConsistencyOK)
	}
}

// registryConsistency runs a consistency check on demand and reports the result.
// It is what `make verify-deploy` asserts against, and the surface a monitor
// polls: a stable place to read "has this deployment's log stayed append-only?".
func (h *handlers) registryConsistency(w http.ResponseWriter, r *http.Request) {
	m, err := consistencyMonitor(h.d)
	if err != nil {
		httpx.Fail(w, h.d.Log, "build consistency monitor", err)
		return
	}
	if m == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"transparent": false,
			"detail":      "this deployment runs on the Postgres fallback; its public facts carry no inclusion or consistency proof",
		})
		return
	}
	res, err := m.Check(r.Context())
	if errors.Is(err, dedi.ErrHistoryRewritten) {
		// The one check whose failure is a 200 with rewritten:true rather than a
		// 5xx: the endpoint answered correctly, and the answer is that the log
		// was rewritten. A caller keys on the field, not the status code.
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"transparent": true, "consistent": false, "rewritten": true,
			"size": res.Size, "root": res.Root, "detail": res.Detail,
		})
		return
	}
	if err != nil {
		httpx.Fail(w, h.d.Log, "check registry consistency", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"transparent": true, "consistent": res.Consistent, "rewritten": false,
		"size": res.Size, "root": res.Root, "authenticated": res.Authenticated,
		"firstObservation":     res.FirstObservation,
		"witnessChecked":       res.WitnessChecked,
		"witnessConsistencyOK": res.WitnessConsistencyOK,
		"detail":               res.Detail,
	})
}
