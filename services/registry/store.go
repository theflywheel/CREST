package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Match is what resolution returns: a party, and how it was found.
//
// "How" is not decoration. §4.1 requires the claim to record which key matched
// and at what confidence, because that provenance is an input the tier function
// reads — a Tier-1 record's stated primary risk is identity mapping error.
type Match struct {
	PartyID    string  `json:"partyId"`
	Key        string  `json:"key"`
	Confidence float64 `json:"confidence"`
}

// Hold is an ambiguous match. It is a row rather than an error because someone
// has to resolve it, and because a queue nobody can list is a queue nobody works.
type Hold struct {
	ID         string    `json:"id"`
	KeyKind    string    `json:"keyKind"`
	KeyValue   string    `json:"keyValue"`
	Candidates []string  `json:"candidates"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Confidence per key kind. A salted national-ID hash is an exact match on a
// nationally unique value; a phone number is a route that gets reassigned; a
// roster id means nothing outside its project. These numbers are the reason a
// definition can require a minimum assurance and get a different answer for
// the same evidence.
const (
	confidenceNationalIDHash = 1.0
	confidenceContactRoute   = 0.8
	confidenceRosterID       = 0.6
)

func insertParty(ctx context.Context, tx store.Querier, p schema.Party) error {
	doc, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO parties (id, kind, doc, created_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET doc = EXCLUDED.doc, kind = EXCLUDED.kind`,
		p.ID, string(p.Kind), doc, p.CreatedAt); err != nil {
		return fmt.Errorf("insert party: %w", err)
	}

	// Keys are derived from the document, never sent separately: two sources of
	// truth for "what can find this person" is how a person acquires a second
	// record.
	if _, err := tx.Exec(ctx, `DELETE FROM party_keys WHERE party_id = $1`, p.ID); err != nil {
		return err
	}
	for _, b := range p.IdentityBindings {
		if b.NationalIDHash == nil {
			continue
		}
		if err := putKey(ctx, tx, p.ID, "national-id-hash", b.NationalIDHash.Value, nil); err != nil {
			return err
		}
	}
	for _, r := range p.ContactRoutes {
		if r.Kind == schema.PartyContactRoutesItemKindPhone || r.Kind == schema.PartyContactRoutesItemKindEmail {
			if err := putKey(ctx, tx, p.ID, "contact-route", r.Value, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func putKey(ctx context.Context, tx store.Querier, partyID, kind, value string, scope *string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO party_keys (party_id, key_kind, key_value, scope_id)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, partyID, kind, value, scope)
	return err
}

// PutRosterID registers a project-scoped roster mapping. Scoped because a
// roster id matched globally is how two people become one person (§4.1).
func putRosterID(ctx context.Context, tx store.Querier, partyID, rosterID, contextID string) error {
	return putKey(ctx, tx, partyID, "roster-id", rosterID, &contextID)
}

func getParty(ctx context.Context, q store.Querier, id string) (schema.Party, error) {
	var doc []byte
	err := q.QueryRow(ctx, `SELECT doc FROM parties WHERE id = $1`, id).Scan(&doc)
	if err != nil {
		return schema.Party{}, err
	}
	var p schema.Party
	return p, json.Unmarshal(doc, &p)
}

// resolve finds the party a joining identifier belongs to.
//
// Precedence is declared here and nowhere else: strongest key first, and the
// first key that produces exactly one candidate wins. Multiple candidates on
// any key is a hold — never a guess, and never a merge (W7).
func resolve(ctx context.Context, q store.Querier, kind, value, contextID string) (Match, []string, error) {
	order := []struct {
		kind       string
		confidence float64
		scoped     bool
	}{
		{"national-id-hash", confidenceNationalIDHash, false},
		{"contact-route", confidenceContactRoute, false},
		{"roster-id", confidenceRosterID, true},
	}
	for _, k := range order {
		if kind != "" && kind != k.kind {
			continue
		}
		var rows store.Rows
		var err error
		if k.scoped {
			rows, err = q.Query(ctx,
				`SELECT party_id FROM party_keys WHERE key_kind = $1 AND key_value = $2 AND scope_id = $3`,
				k.kind, value, contextID)
		} else {
			rows, err = q.Query(ctx,
				`SELECT party_id FROM party_keys WHERE key_kind = $1 AND key_value = $2`, k.kind, value)
		}
		if err != nil {
			return Match{}, nil, err
		}
		ids, err := store.Collect(rows, func(r store.Row) (string, error) {
			var v string
			return v, r.Scan(&v)
		})
		rows.Close()
		if err != nil {
			return Match{}, nil, err
		}
		switch len(ids) {
		case 0:
			continue
		case 1:
			return Match{PartyID: ids[0], Key: k.kind, Confidence: k.confidence}, nil, nil
		default:
			return Match{}, ids, nil
		}
	}
	return Match{}, nil, store.ErrNotFound
}

func insertHold(ctx context.Context, tx store.Querier, h Hold) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO match_holds (id, key_kind, key_value, candidates, reason, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		h.ID, h.KeyKind, h.KeyValue, h.Candidates, h.Reason, h.CreatedAt)
	return err
}

func openHolds(ctx context.Context, q store.Querier) ([]Hold, error) {
	rows, err := q.Query(ctx,
		`SELECT id, key_kind, key_value, candidates, reason, created_at
		 FROM match_holds WHERE resolved_at IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (Hold, error) {
		var h Hold
		return h, r.Scan(&h.ID, &h.KeyKind, &h.KeyValue, &h.Candidates, &h.Reason, &h.CreatedAt)
	})
}

func insertAuthorization(ctx context.Context, tx store.Querier, a schema.Authorization) error {
	doc, err := json.Marshal(a)
	if err != nil {
		return err
	}
	var contextID *string
	if a.Scope.ContextID != nil {
		contextID = a.Scope.ContextID
	}
	var end *time.Time
	if a.Period.End != nil {
		end = a.Period.End
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO authorizations
		   (id, party_id, scope_kind, context_id, functions, period_start, period_end, state, doc)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, doc = EXCLUDED.doc`,
		a.ID, a.PartyID, string(a.Scope.Kind), contextID, a.Functions,
		a.Period.Start, end, string(a.State), doc)
	return err
}

// permits answers the question the evidence pipeline actually asks: may this
// party submit evidence here, right now?
//
// All three conditions are checked in one place because checking two of them is
// the same as checking none: an expired authorization and a revoked one both
// look active if you only read the state column.
func permits(ctx context.Context, q store.Querier, partyID, function, contextID string, at time.Time) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM authorizations
		WHERE party_id = $1
		  AND state = 'ACTIVE'
		  AND $2 = ANY (functions)
		  AND period_start <= $4
		  AND (period_end IS NULL OR period_end >= $4)
		  -- An instance-scoped authorization covers every context; a
		  -- context-scoped one covers only its own, and is always a strict
		  -- subset of an instance one (§2).
		  AND (scope_kind = 'instance' OR context_id = $3)`,
		partyID, function, contextID, at).Scan(&n)
	return n > 0, err
}

func insertContext(ctx context.Context, tx store.Querier, c schema.Context) error {
	doc, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO contexts (id, kind, state, doc) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, doc = EXCLUDED.doc`,
		c.ID, c.Kind, string(c.State), doc)
	return err
}

func insertTerms(ctx context.Context, tx store.Querier, t schema.Terms) error {
	doc, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO terms (id, version, doc, published_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id, version) DO NOTHING`, t.ID, t.Version, doc, t.PublishedAt)
	return err
}

func getTerms(ctx context.Context, q store.Querier, id string, version int) (schema.Terms, error) {
	var doc []byte
	err := q.QueryRow(ctx,
		`SELECT doc FROM terms WHERE id = $1 AND version = $2`, id, version).Scan(&doc)
	if err != nil {
		return schema.Terms{}, err
	}
	var t schema.Terms
	return t, json.Unmarshal(doc, &t)
}

func getAuthorization(ctx context.Context, q store.Querier, id string) (schema.Authorization, error) {
	var doc []byte
	err := q.QueryRow(ctx, `SELECT doc FROM authorizations WHERE id = $1`, id).Scan(&doc)
	if err != nil {
		return schema.Authorization{}, err
	}
	var a schema.Authorization
	return a, json.Unmarshal(doc, &a)
}

// Publication is where one public fact landed on the registry substrate.
type Publication struct {
	Kind            string    `json:"kind"`
	SubjectID       string    `json:"subjectId"`
	SubjectVersion  int       `json:"subjectVersion"`
	Namespace       string    `json:"namespace"`
	Registry        string    `json:"registry"`
	Record          string    `json:"record"`
	RegistryVersion string    `json:"registryVersion"`
	Digest          string    `json:"digest"`
	State           string    `json:"state"`
	Transparent     bool      `json:"transparent"`
	PublishedAt     time.Time `json:"publishedAt"`
}

func recordPublication(ctx context.Context, db *store.DB, msg factMessage, r dedi.Receipt, at time.Time) error {
	return db.InTx(ctx, func(tx store.Querier) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO registry_publications
				(subject_kind, subject_id, subject_version, namespace, registry, record,
				 registry_version, digest, state, transparent, published_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (subject_kind, subject_id, subject_version) DO UPDATE SET
				registry_version = EXCLUDED.registry_version,
				digest           = EXCLUDED.digest,
				state            = EXCLUDED.state,
				transparent      = EXCLUDED.transparent,
				published_at     = EXCLUDED.published_at`,
			msg.Kind, msg.ID, msg.Version, r.Ref.Namespace, r.Ref.Registry, r.Ref.Record,
			r.Ref.Version, r.Digest, r.State, r.Transparent, at)
		return err
	})
}

func publicationOf(ctx context.Context, q store.Querier, kind, id string, version int) (Publication, error) {
	var p Publication
	err := q.QueryRow(ctx, `
		SELECT subject_kind, subject_id, subject_version, namespace, registry, record,
		       registry_version, digest, state, transparent, published_at
		FROM registry_publications
		WHERE subject_kind = $1 AND subject_id = $2 AND subject_version = $3`,
		kind, id, version).
		Scan(&p.Kind, &p.SubjectID, &p.SubjectVersion, &p.Namespace, &p.Registry, &p.Record,
			&p.RegistryVersion, &p.Digest, &p.State, &p.Transparent, &p.PublishedAt)
	return p, err
}
