package definitions

// The governance event record: append-only, one actor per row.
//
// The definition document shows what the current state is; this table shows
// how it got there — who submitted, who ratified naming which pending fields,
// who activated. There is no UPDATE or DELETE path through this file, and the
// two-records-one-signature flow (p3_16) is exactly this: the ratify call
// mutates the version's state and appends the event, in one transaction, so
// the signature either produced both records or neither.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

// Event is one recorded governance act on a definition version.
type Event struct {
	ID           int64          `json:"id"`
	DefinitionID string         `json:"definitionId"`
	Version      int            `json:"version"`
	Action       string         `json:"action"`
	ActorPartyID string         `json:"actorPartyId"`
	At           time.Time      `json:"at"`
	Detail       map[string]any `json:"detail,omitempty"`
}

const (
	eventSubmitted = "SUBMITTED"
	eventCreated   = "CREATED"
	eventRatified  = "RATIFIED"
	eventActivated = "ACTIVATED"
	eventHandoff   = "PAYMENT_HANDOFF"
)

func appendEvent(ctx context.Context, tx store.Querier, defID string, version int,
	action, actor string, at time.Time, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO definition_events (definition_id, version, action, actor_party_id, at, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		defID, version, action, actor, at, raw)
	return err
}

func listEvents(ctx context.Context, q store.Querier, defID string) ([]Event, error) {
	rows, err := q.Query(ctx, `
		SELECT id, definition_id, version, action, actor_party_id, at, detail
		FROM definition_events WHERE definition_id = $1 ORDER BY id`, defID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (Event, error) {
		var e Event
		var raw []byte
		if err := r.Scan(&e.ID, &e.DefinitionID, &e.Version, &e.Action, &e.ActorPartyID, &e.At, &raw); err != nil {
			return Event{}, err
		}
		return e, json.Unmarshal(raw, &e.Detail)
	})
}
