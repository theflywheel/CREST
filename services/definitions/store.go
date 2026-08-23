package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// ErrImmutable is returned for any attempt to change a definition that has
// reached ACTIVE. It is a distinct error because it is not a validation
// failure: the document may be perfectly valid, and still must not land.
var ErrImmutable = errors.New("an ACTIVE definition is immutable; publish a new version")

// ErrSelfRatified is separation of duties, refused by name so the caller can
// say something better than "constraint violation" (§7).
var ErrSelfRatified = errors.New("the author of a version may not ratify it")

func insertDefinition(ctx context.Context, tx store.Querier, d schema.Definition) error {
	doc, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO definitions (id, version, state, activity_code, authored_by, ratified_by, doc, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.Version, string(d.State), d.Activity.Code,
		d.AuthoredByPartyID, d.RatifiedByPartyID, doc, d.CreatedAt)
	return err
}

func getDefinition(ctx context.Context, q store.Querier, defID string, version int) (schema.Definition, error) {
	var doc []byte
	var err error
	if version > 0 {
		err = q.QueryRow(ctx,
			`SELECT doc FROM definitions WHERE id = $1 AND version = $2`, defID, version).Scan(&doc)
	} else {
		// No version asked for means the one in force. Not "the newest row":
		// a DRAFT of v2 must not answer for an ACTIVE v1, or every verifier
		// resolving a credential would follow the draft.
		err = q.QueryRow(ctx, `
			SELECT doc FROM definitions
			WHERE id = $1 AND state = 'ACTIVE'
			ORDER BY version DESC LIMIT 1`, defID).Scan(&doc)
	}
	if err != nil {
		return schema.Definition{}, err
	}
	var d schema.Definition
	return d, json.Unmarshal(doc, &d)
}

// transition moves a version between states, refusing anything an ACTIVE
// definition should not permit. The state is read and written in one statement
// so two concurrent activations cannot both see DRAFT.
func transition(ctx context.Context, tx store.Querier, defID string, version int,
	from, to schema.DefinitionState, mutate func(*schema.Definition) error) (schema.Definition, error) {
	var doc []byte
	var state string
	err := tx.QueryRow(ctx,
		`SELECT doc, state FROM definitions WHERE id = $1 AND version = $2 FOR UPDATE`,
		defID, version).Scan(&doc, &state)
	if err != nil {
		return schema.Definition{}, err
	}
	if schema.DefinitionState(state) == schema.DefinitionStateACTIVE && to != schema.DefinitionStateSUPERSEDED {
		return schema.Definition{}, ErrImmutable
	}
	if schema.DefinitionState(state) != from {
		return schema.Definition{}, fmt.Errorf("version %d is %s, not %s", version, state, from)
	}

	var d schema.Definition
	if err := json.Unmarshal(doc, &d); err != nil {
		return schema.Definition{}, err
	}
	d.State = to
	if mutate != nil {
		if err := mutate(&d); err != nil {
			return schema.Definition{}, err
		}
	}
	updated, err := json.Marshal(d)
	if err != nil {
		return schema.Definition{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE definitions SET state = $3, ratified_by = $4, doc = $5 WHERE id = $1 AND version = $2`,
		defID, version, string(d.State), d.RatifiedByPartyID, updated); err != nil {
		return schema.Definition{}, err
	}
	return d, nil
}

func insertLinkedRecord(ctx context.Context, tx store.Querier, defID string, lr schema.LinkedRecord) error {
	doc, err := json.Marshal(lr)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO definition_linked_records (id, definition_id, type, version, state, doc, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, doc = EXCLUDED.doc`,
		lr.ID, defID, lr.Type, lr.Version, lr.State, doc, lr.CreatedAt)
	return err
}

func linkedRecords(ctx context.Context, q store.Querier, defID, recordType string) ([]schema.LinkedRecord, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM definition_linked_records
		WHERE definition_id = $1 AND ($2 = '' OR type = $2)
		ORDER BY created_at`, defID, recordType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.LinkedRecord, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.LinkedRecord{}, err
		}
		var lr schema.LinkedRecord
		return lr, json.Unmarshal(doc, &lr)
	})
}
