package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/theflywheel/crest/adapters"

	"github.com/theflywheel/crest/pkg/store"
)

// Source heartbeat monitoring (#22, Blueprint §8).
//
// See the migration for why this exists. The short version: a source going
// quiet is the only failure in this system that produces nothing a worker can
// see or dispute, so it is the only one the system has to notice by itself.

// SourceState is derived, never stored.
//
// A stored state is wrong the moment the clock moves, and the whole subject
// here is time passing. It is the same rule as the tier and a Party's identity
// assurance: store the facts — when we last heard from it, how often we expect
// to — and compute the judgement on request.
type SourceState string

const (
	// SourceHealthy has been heard from within its declared cadence.
	SourceHealthy SourceState = "HEALTHY"
	// SourceSilent has not, and somebody is expected to do something about it.
	SourceSilent SourceState = "SILENT"
	// SourceNeverSeen was registered and has never sent anything. Distinct from
	// SILENT on purpose: a source that has gone quiet is a broken integration,
	// and one that never started is a deployment step nobody finished. They
	// look identical in a count and need different people.
	SourceNeverSeen SourceState = "NEVER_SEEN"
)

// Source is a feed this deployment expects evidence from.
type Source struct {
	ID            string `json:"id"`
	AdapterRef    string `json:"adapterRef"`
	ContextID     string `json:"contextId"`
	SystemRef     string `json:"systemRef"`
	ExpectedEvery string `json:"expectedEvery"`
	OwnerPartyID  string `json:"ownerPartyId"`

	// Mapping is how this source's own column names reach the canonical record
	// (#25). Configuration, held beside the rest of what this deployment knows
	// about this feed rather than in a file somewhere else: the cadence, the
	// owner and the vocabulary are all facts about the same integration, and
	// splitting them is how one gets updated and the others do not.
	Mapping adapters.Mapping `json:"mapping"`

	RegisteredAt time.Time  `json:"registeredAt"`
	LastSeenAt   *time.Time `json:"lastSeenAt,omitempty"`
	SilentSince  *time.Time `json:"silentSince,omitempty"`
	NotifiedAt   *time.Time `json:"notifiedAt,omitempty"`

	// Computed on read.
	State    SourceState `json:"state"`
	QuietFor string      `json:"quietFor,omitempty"`

	expectedEvery time.Duration
}

// stateAt derives the state. Exported behaviour, unexported mechanism: the
// caller passes the clock in, so a harness that moves time gets the same answer
// a production sweep would at that instant.
func (s *Source) stateAt(now time.Time) {
	if s.LastSeenAt == nil {
		s.State = SourceNeverSeen
		s.QuietFor = now.Sub(s.RegisteredAt).Round(time.Minute).String()
		return
	}
	quiet := now.Sub(*s.LastSeenAt)
	if quiet > s.expectedEvery {
		s.State = SourceSilent
		s.QuietFor = quiet.Round(time.Minute).String()
		return
	}
	s.State = SourceHealthy
}

// overdue reports whether a source should be counted as silent. NEVER_SEEN
// counts: a source registered a month ago that has never sent a row is not
// waiting patiently, it is broken.
func (s *Source) overdue(now time.Time) bool {
	return s.State == SourceSilent || (s.State == SourceNeverSeen && now.Sub(s.RegisteredAt) > s.expectedEvery)
}

// ErrBadCadence refuses a source registration whose declared cadence could
// never fire — a zero or negative interval is a heartbeat nobody can miss.
var ErrBadCadence = errors.New("expectedEvery must be a positive duration")

// registerSource upserts and returns the row as it now stands.
//
// It returns the STORED id, not the one the caller minted. Re-registering an
// existing feed keeps its identity — that is the point of the upsert — so
// handing back a fresh id would give the caller a key that matches nothing, and
// every subsequent lookup would silently find nothing at all.
func registerSource(ctx context.Context, tx store.Querier, s Source) (Source, error) {
	if s.expectedEvery <= 0 {
		return Source{}, ErrBadCadence
	}
	mappingJSON, err := json.Marshal(s.Mapping)
	if err != nil {
		return Source{}, fmt.Errorf("marshal source mapping: %w", err)
	}
	// Re-registering updates the cadence and the owner and leaves the history
	// alone. A source whose owner changed hands is the same source, and
	// resetting last_seen_at would make it look healthy for one cadence for no
	// reason other than that somebody edited a row.
	var secs int64
	err = tx.QueryRow(ctx, `
		INSERT INTO sources (id, adapter_ref, context_id, system_ref, expected_every,
		                     owner_party_id, registered_at, mapping)
		VALUES ($1,$2,$3,$4,$5::interval,$6,$7,$8)
		ON CONFLICT (adapter_ref, context_id) DO UPDATE SET
			expected_every = EXCLUDED.expected_every,
			owner_party_id = EXCLUDED.owner_party_id,
			system_ref     = EXCLUDED.system_ref,
			mapping        = EXCLUDED.mapping
		RETURNING id, registered_at, last_seen_at,
		          extract(epoch from expected_every)::bigint`,
		s.ID, s.AdapterRef, s.ContextID, s.SystemRef,
		fmt.Sprintf("%d seconds", int64(s.expectedEvery.Seconds())), s.OwnerPartyID, s.RegisteredAt,
		mappingJSON).
		Scan(&s.ID, &s.RegisteredAt, &s.LastSeenAt, &secs)
	if err != nil {
		return Source{}, err
	}
	s.expectedEvery = time.Duration(secs) * time.Second
	s.ExpectedEvery = s.expectedEvery.String()
	return s, nil
}

// markSeen records a heartbeat, and clears any silence episode.
//
// Called on every accepted batch, in the same transaction. A heartbeat written
// outside the transaction that accepted the evidence could record a source as
// alive on a batch that was then rolled back.
//
// A source nobody registered is not created here. Auto-registering on first
// sight would need a cadence to be invented, and an invented cadence produces
// alerts nobody believes — which is worse than no alert, because it teaches
// people to ignore the real one. The consequence is stated in the manifest: an
// unregistered source is not watched.
func markSeen(ctx context.Context, tx store.Querier, adapterRef, contextID string, at time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE sources SET last_seen_at = $3, silent_since = NULL, notified_at = NULL
		WHERE adapter_ref = $1 AND context_id = $2`, adapterRef, contextID, at)
	return err
}

func listSources(ctx context.Context, q store.Querier, now time.Time) ([]Source, error) {
	rows, err := q.Query(ctx, `
		SELECT id, adapter_ref, context_id, system_ref,
		       extract(epoch from expected_every)::bigint,
		       owner_party_id, registered_at, last_seen_at, silent_since, notified_at, mapping
		FROM sources ORDER BY adapter_ref, context_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := store.Collect(rows, func(r store.Row) (Source, error) {
		var s Source
		var secs int64
		var mapping []byte
		if err := r.Scan(&s.ID, &s.AdapterRef, &s.ContextID, &s.SystemRef, &secs,
			&s.OwnerPartyID, &s.RegisteredAt, &s.LastSeenAt, &s.SilentSince, &s.NotifiedAt,
			&mapping); err != nil {
			return Source{}, err
		}
		if len(mapping) > 0 {
			if err := json.Unmarshal(mapping, &s.Mapping); err != nil {
				return Source{}, fmt.Errorf("source %s has an unreadable mapping: %w", s.ID, err)
			}
		}
		s.expectedEvery = time.Duration(secs) * time.Second
		s.ExpectedEvery = s.expectedEvery.String()
		s.stateAt(now)
		return s, nil
	})
	return out, err
}

// mappingFor is the configured vocabulary for one feed, or an empty mapping.
//
// An empty mapping is not a failure: a file that already speaks CREST's column
// names needs no translation, which is every fixture in this repo and the case
// this started as. Registering the source is what a deployment does when its
// source system speaks its own.
func mappingFor(ctx context.Context, q store.Querier, adapterRef, contextID string) (adapters.Mapping, error) {
	var raw []byte
	err := q.QueryRow(ctx,
		`SELECT mapping FROM sources WHERE adapter_ref = $1 AND context_id = $2`,
		adapterRef, contextID).Scan(&raw)
	if errors.Is(err, store.ErrNotFound) || len(raw) == 0 {
		return adapters.Mapping{}, nil
	}
	if err != nil {
		return adapters.Mapping{}, err
	}
	var m adapters.Mapping
	return m, json.Unmarshal(raw, &m)
}

// openSilence records that a source has gone quiet, and reports whether this
// call is the one that discovered it.
//
// The distinction is the whole design of the alert. A sweep runs often; a
// source can be silent for weeks. Alerting on the *state* means one message per
// sweep until somebody fixes it, which is how an alert channel becomes
// something people mute. Alerting on the *transition* means one message per
// episode, and the episode is a row so "when did this start" has an answer
// after the fact.
func openSilence(ctx context.Context, tx store.Querier, sourceID string, at time.Time) (bool, error) {
	n, err := tx.Exec(ctx, `
		UPDATE sources SET silent_since = $2, notified_at = $2
		WHERE id = $1 AND silent_since IS NULL`, sourceID, at)
	return n > 0, err
}
