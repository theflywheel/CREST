package parties

import (
	"context"
	"encoding/json"
	"time"

	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Storage for the project surface. The document is the record and the columns
// are an index, exactly as 0001_init.sql says — so every read below returns
// the Context document and no read reconstructs one from columns.

func getContext(ctx context.Context, q store.Querier, id string) (schema.Context, error) {
	return getContextLocked(ctx, q, id, false)
}

// getContextLocked with forUpdate holds the row for the read-modify-write the
// acknowledgement and activation paths perform. Two configurators answering
// the same handover at the same moment would otherwise both read PENDING.
func getContextLocked(ctx context.Context, q store.Querier, id string, forUpdate bool) (schema.Context, error) {
	sql := `SELECT doc FROM contexts WHERE id = $1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	var doc []byte
	if err := q.QueryRow(ctx, sql, id).Scan(&doc); err != nil {
		return schema.Context{}, err
	}
	var c schema.Context
	return c, json.Unmarshal(doc, &c)
}

// contextQuery is a listing narrowed to one party's stake in it.
//
// Either owner or configurator is always set, and the handler authorizes
// against whichever it is. An unnarrowed listing would answer "which projects
// does this deployment run, and who configures each" to any signed-in caller —
// the same membership oracle #68 closed on the authorization listing, in a
// different shape.
type contextQuery struct {
	OwnerPartyID        string
	ConfiguratorPartyID string
	Kind                string
	State               string
	OwnershipState      string
}

func listContexts(ctx context.Context, q store.Querier, f contextQuery) ([]schema.Context, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM contexts
		WHERE ($1 = '' OR owner_party_id = $1)
		  AND ($2 = '' OR configurator_party_id = $2)
		  AND ($3 = '' OR kind = $3)
		  AND ($4 = '' OR state = $4)
		  AND ($5 = '' OR ownership_state = $5)
		ORDER BY id`,
		f.OwnerPartyID, f.ConfiguratorPartyID, f.Kind, f.State, f.OwnershipState)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Context, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.Context{}, err
		}
		var c schema.Context
		return c, json.Unmarshal(doc, &c)
	})
}

// appendOwnershipEvent writes one row of the acknowledgement trail. The
// sequence is taken inside the same transaction as the context update, so the
// trail cannot end up with a gap or a duplicate where two handovers raced.
func appendOwnershipEvent(ctx context.Context, tx store.Querier, contextID string, ev ownershipEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO context_ownership_events (context_id, seq, event, party_id, actor_party_id, reason, at)
		VALUES ($1,
		        (SELECT coalesce(max(seq), 0) + 1 FROM context_ownership_events WHERE context_id = $1),
		        $2, $3, $4, $5, $6)`,
		contextID, ev.Event, ev.PartyID, ev.ActorPartyID, ev.Reason, ev.At)
	return err
}

func ownershipEvents(ctx context.Context, q store.Querier, contextID string) ([]ownershipEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT seq, event, party_id, actor_party_id, reason, at
		FROM context_ownership_events WHERE context_id = $1 ORDER BY seq`, contextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (ownershipEvent, error) {
		var e ownershipEvent
		return e, r.Scan(&e.Seq, &e.Event, &e.PartyID, &e.ActorPartyID, &e.Reason, &e.At)
	})
}

// contextRecord is one configuration-level fact keyed to a context, with the
// two things that make it accountable rather than merely present: who
// recorded it and when.
type contextRecord struct {
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	RecordedBy string          `json:"recordedBy"`
	RecordedAt time.Time       `json:"recordedAt"`
}

func putContextRecord(ctx context.Context, tx store.Querier, contextID, kind string,
	payload any, by string, at time.Time) error {

	blob, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO context_records (context_id, kind, payload, recorded_by, recorded_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (context_id, kind) DO UPDATE SET
		    payload = EXCLUDED.payload,
		    recorded_by = EXCLUDED.recorded_by,
		    recorded_at = EXCLUDED.recorded_at`,
		contextID, kind, blob, by, at)
	return err
}

func getContextRecord(ctx context.Context, q store.Querier, contextID, kind string) (contextRecord, error) {
	var rec contextRecord
	err := q.QueryRow(ctx, `
		SELECT kind, payload, recorded_by, recorded_at
		FROM context_records WHERE context_id = $1 AND kind = $2`, contextID, kind).
		Scan(&rec.Kind, &rec.Payload, &rec.RecordedBy, &rec.RecordedAt)
	return rec, err
}

// listContextRecords returns every record whose kind starts with the prefix —
// the composition read, which wants the choices and not the finance link.
func listContextRecords(ctx context.Context, q store.Querier, contextID, prefix string) ([]contextRecord, error) {
	rows, err := q.Query(ctx, `
		SELECT kind, payload, recorded_by, recorded_at
		FROM context_records
		WHERE context_id = $1 AND ($2 = '' OR kind LIKE $2 || '%')
		ORDER BY kind`, contextID, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (contextRecord, error) {
		var rec contextRecord
		return rec, r.Scan(&rec.Kind, &rec.Payload, &rec.RecordedBy, &rec.RecordedAt)
	})
}

// contextGrants lists the authorizations scoped to one context, whatever their
// state.
//
// Revoked and expired grants are returned rather than filtered, because "a
// role is held, not just recorded" cuts both ways: a console that shows only
// live grants cannot answer "who used to be able to do this, and who took it
// away". The handler renders state alongside grantor and grant date and lets
// the reader see the difference.
func contextGrants(ctx context.Context, q store.Querier, contextID string) ([]schema.Authorization, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM authorizations
		WHERE scope_kind = 'context' AND context_id = $1
		ORDER BY period_start, id`, contextID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Authorization, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.Authorization{}, err
		}
		var a schema.Authorization
		return a, json.Unmarshal(doc, &a)
	})
}

// authorityGrants lists the authorizations one organisation granted at
// instance scope — "who holds a role under this organisation", which is p1_2's
// read and the one the existing GET /v1/authorizations cannot answer: that
// endpoint keys on the party who HOLDS the grant, and this question keys on
// the party who GAVE it.
//
// This is deliberately not the roster query #68 closed. That refusal was about
// answering "where does this person work" for an arbitrary person, from the
// public log or from an unscoped listing. This answers "who did YOU grant
// roles to" to the granting organisation itself, and the handler proves the
// caller is — or may act for — that organisation before it runs.
func authorityGrants(ctx context.Context, q store.Querier, authorityPartyID string) ([]schema.Authorization, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM authorizations
		WHERE scope_kind = 'instance' AND doc->>'authorityPartyId' = $1
		ORDER BY period_start, id`, authorityPartyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Authorization, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return schema.Authorization{}, err
		}
		var a schema.Authorization
		return a, json.Unmarshal(doc, &a)
	})
}

// approvedOrganisations is the partner directory's source (p2_17): the
// organisations this registry approved, with the terms they accepted and the
// permissions those terms carry.
//
// APPROVED only, and by join rather than by filter in Go, so an organisation
// that applied and was never decided cannot appear in a directory a
// configurator reads as a list of who may be invited.
func approvedOrganisations(ctx context.Context, q store.Querier) ([]directoryEntry, error) {
	rows, err := q.Query(ctx, `
		SELECT p.doc, r.terms_id, r.terms_version, t.doc, r.decided_at
		FROM org_registrations r
		JOIN parties p ON p.id = r.party_id
		JOIN terms t ON t.id = r.terms_id AND t.version = r.terms_version
		WHERE r.state = 'APPROVED'
		ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (directoryEntry, error) {
		var partyDoc, termsDoc []byte
		var termsID string
		var termsVersion int
		var decidedAt *time.Time
		if err := r.Scan(&partyDoc, &termsID, &termsVersion, &termsDoc, &decidedAt); err != nil {
			return directoryEntry{}, err
		}
		var p schema.Party
		if err := json.Unmarshal(partyDoc, &p); err != nil {
			return directoryEntry{}, err
		}
		var t schema.Terms
		if err := json.Unmarshal(termsDoc, &t); err != nil {
			return directoryEntry{}, err
		}
		return directoryEntry{
			PartyID:      p.ID,
			DisplayName:  p.DisplayName,
			Sector:       attrString(p.Attributes, "sector"),
			Country:      attrString(p.Attributes, "country"),
			TermsID:      termsID,
			TermsVersion: termsVersion,
			Permissions:  t.Permissions,
			ApprovedAt:   decidedAt,
		}, nil
	})
}

// acceptedTerms returns the exact terms version an organisation accepted, for
// the narrowing check a partner grant runs. Absent terms is not an empty
// permission set — it is an organisation that has not agreed to anything, and
// the caller must tell those apart.
func acceptedTerms(ctx context.Context, q store.Querier, partyID string) (schema.Terms, error) {
	reg, err := getRegistration(ctx, q, partyID)
	if err != nil {
		return schema.Terms{}, err
	}
	if reg.TermsID == nil || reg.TermsVersion == nil {
		return schema.Terms{}, store.ErrNotFound
	}
	return getTerms(ctx, q, *reg.TermsID, *reg.TermsVersion)
}
