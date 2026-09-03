package definitions

// Storage for drafts: the one mutable object in this service, and mutable
// only while OPEN. A SUBMITTED draft is kept as the provenance of the version
// it produced; nothing ever flows from here back into a definitions row.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

// Draft states. OPEN is the only state in which a section write lands.
const (
	draftOpen      = "OPEN"
	draftSubmitted = "SUBMITTED"
	draftDiscarded = "DISCARDED"
)

// ErrDraftClosed refuses writes to a draft that has been submitted or
// discarded. A submitted draft is a historical record: editing it would edit
// the provenance of a version that already exists.
var ErrDraftClosed = errors.New("this draft is no longer open; clone it into a new draft instead")

// Draft is the stored authoring document with its lifecycle facts.
type Draft struct {
	ID               string    `json:"id"`
	DefinitionID     string    `json:"definitionId,omitempty"`
	BaseVersion      int       `json:"baseVersion,omitempty"`
	State            string    `json:"state"`
	Doc              DraftDoc  `json:"doc"`
	CreatedBy        string    `json:"createdByPartyId"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	SubmittedVersion int       `json:"submittedVersion,omitempty"`
}

func insertDraft(ctx context.Context, tx store.Querier, d Draft) error {
	doc, err := json.Marshal(d.Doc)
	if err != nil {
		return err
	}
	var defID, baseVersion any
	if d.DefinitionID != "" {
		defID = d.DefinitionID
	}
	if d.BaseVersion != 0 {
		baseVersion = d.BaseVersion
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO definition_drafts (id, definition_id, base_version, state, doc, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, defID, baseVersion, d.State, doc, d.CreatedBy, d.CreatedAt, d.UpdatedAt)
	return err
}

func getDraft(ctx context.Context, q store.Querier, id string) (Draft, error) {
	var d Draft
	var doc []byte
	var defID *string
	var baseVersion, submittedVersion *int
	err := q.QueryRow(ctx, `
		SELECT id, definition_id, base_version, state, doc, created_by, created_at, updated_at, submitted_version
		FROM definition_drafts WHERE id = $1`, id).
		Scan(&d.ID, &defID, &baseVersion, &d.State, &doc, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
			&submittedVersion)
	if err != nil {
		return Draft{}, err
	}
	if defID != nil {
		d.DefinitionID = *defID
	}
	if baseVersion != nil {
		d.BaseVersion = *baseVersion
	}
	if submittedVersion != nil {
		d.SubmittedVersion = *submittedVersion
	}
	return d, json.Unmarshal(doc, &d.Doc)
}

func listDrafts(ctx context.Context, q store.Querier, state string) ([]Draft, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM definition_drafts
		WHERE ($1 = '' OR state = $1) ORDER BY created_at DESC`, state)
	if err != nil {
		return nil, err
	}
	ids, err := store.Collect(rows, func(r store.Row) (string, error) {
		var id string
		return id, r.Scan(&id)
	})
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]Draft, 0, len(ids))
	for _, id := range ids {
		d, err := getDraft(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// updateDraftDoc replaces the document of an OPEN draft. The state check and
// the write are one statement, so a submit racing a section write cannot
// leave a submitted draft with a document its submission never saw.
func updateDraftDoc(ctx context.Context, tx store.Querier, id string, doc DraftDoc, at time.Time) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `
		UPDATE definition_drafts SET doc = $2, updated_at = $3
		WHERE id = $1 AND state = 'OPEN'`, id, raw, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		// Either no such draft or not open; the caller reads which.
		if _, err := getDraft(ctx, tx, id); err != nil {
			return err
		}
		return ErrDraftClosed
	}
	return nil
}

// closeDraft moves an OPEN draft to SUBMITTED (recording the version it
// became) or DISCARDED.
func closeDraft(ctx context.Context, tx store.Querier, id, state string, submittedVersion int, at time.Time) error {
	var sv any
	if submittedVersion != 0 {
		sv = submittedVersion
	}
	affected, err := tx.Exec(ctx, `
		UPDATE definition_drafts SET state = $2, submitted_version = $3, updated_at = $4
		WHERE id = $1 AND state = 'OPEN'`, id, state, sv, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		if _, err := getDraft(ctx, tx, id); err != nil {
			return err
		}
		return ErrDraftClosed
	}
	return nil
}

// setDraftDefinition records at submit which definition id the draft became
// part of, for a draft that started with no definition.
func setDraftDefinition(ctx context.Context, tx store.Querier, id, defID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE definition_drafts SET definition_id = $2 WHERE id = $1`, id, defID)
	return err
}

// nextVersion is the version a submit takes for an existing definition: one
// past the highest row, never a rewrite of one that exists.
func nextVersion(ctx context.Context, q store.Querier, defID string) (int, error) {
	var max int
	err := q.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM definitions WHERE id = $1`, defID).Scan(&max)
	return max + 1, err
}
