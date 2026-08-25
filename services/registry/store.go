package main

import (
	"context"
	"encoding/json"
	"errors"
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

	// EnrolmentConsent travels with the match because evidence has to know it
	// for every row, and asking separately would be a second call per row
	// against the same party. §9 makes enrolment consent the right to fetch and
	// hold evidence about this worker, so it is exactly as relevant to "who is
	// this row about" as the identity is.
	EnrolmentConsent ConsentState `json:"enrolmentConsent"`
}

// Hold is an ambiguous match. It is a row rather than an error because someone
// has to resolve it, and because a queue nobody can list is a queue nobody works.
// Hold is a duplicate the registry refused to resolve on its own.
//
// KeyValue is deliberately not serialised. Blueprint §16 asks that duplicate
// holds show existence rather than content, and the reason is concrete: the
// value is a phone number for a contact-route hold, and a queue that anybody
// can list would otherwise hand out the phone numbers of every worker two
// records disagree about. A custodian deciding a hold needs to know that two
// parties collide on a contact route — not what the route is. `keyKind` says
// which kind of identifier collided, and the hold's own id distinguishes one
// hold from another, so nothing about working the queue needs the value.
type Hold struct {
	ID         string    `json:"id"`
	KeyKind    string    `json:"keyKind"`
	KeyValue   string    `json:"-"`
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
	//
	// Only the kinds the document actually carries. Roster ids live in this
	// table and nowhere else — they are registered through their own endpoint
	// and are scoped to a context — so a rebuild from the document has no
	// authority over them. Deleting them here would silently unregister a
	// worker from their project's roster on any later write to the party, and
	// the visible symptom would be their evidence landing in the unclear queue
	// with nothing to say why.
	//
	// identity-subject is rebuilt here with the other document-derived kinds.
	// It is what a bearer token resolves through (#89), and rebuilding it from
	// the document is what keeps it honest: a subject that is no longer in
	// identityBindings is a subject nobody should still be able to log in as.
	if _, err := tx.Exec(ctx,
		`DELETE FROM party_keys WHERE party_id = $1
		 AND key_kind IN ('national-id-hash', 'contact-route', 'identity-subject')`, p.ID); err != nil {
		return err
	}
	for _, b := range p.IdentityBindings {
		if authenticating(b.ProviderClass) && b.SubjectRef != "" {
			if err := putKey(ctx, tx, p.ID, keyIdentitySubject, b.SubjectRef, nil); err != nil {
				return err
			}
		}
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
		// Candidates are mapped through any merge before they are counted.
		// Two parties that turned out to be one person stop producing a hold
		// the moment the merge is recorded — without this the queue would
		// re-raise the same duplicate on the next batch, and a custodian would
		// be asked to decide something they already decided.
		ids, err = followMerges(ctx, q, ids)
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

// authorizationsFor finds an organisation's authorizations at a given scope.
//
// This is what makes the credential's issuerAuthority resolvable (#16): a
// verifier walking up from a credential needs the authorization that stands
// behind it, and it has to be one that was actually published — which, per #68,
// means one held by an organisation rather than by a person.
//
// Scoped queries only. There is deliberately no "all authorizations for this
// party" here, because the caller that wanted that would be a caller browsing a
// roster.
func authorizationsFor(ctx context.Context, q store.Querier,
	partyID, scopeKind, contextID string, at time.Time) ([]schema.Authorization, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM authorizations
		WHERE party_id = $1
		  AND scope_kind = $2
		  AND ($3 = '' OR context_id = $3)
		  AND state = 'ACTIVE'
		  AND period_start <= $4
		  AND (period_end IS NULL OR period_end >= $4)
		ORDER BY id`, partyID, scopeKind, contextID, at)
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

// followMerges replaces every party id with the survivor it was merged into,
// and drops the duplicates that collapse produces.
//
// Chains are followed, because a second merge onto an already-absorbed party is
// a thing a custodian can legitimately do over time. The bound is there because
// a cycle in this data would otherwise hang a resolve, and a resolve that hangs
// stops evidence ingestion for everybody: the resolution endpoint refuses to
// merge into an already-merged party, so a chain this long means the table has
// been written by something other than that endpoint, and the honest response
// is an error rather than a guess.
const maxMergeDepth = 16

func followMerges(ctx context.Context, q store.Querier, ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := raw
		for depth := 0; ; depth++ {
			if depth > maxMergeDepth {
				return nil, fmt.Errorf("merge chain from %s is longer than %d: "+
					"the parties table holds a cycle", raw, maxMergeDepth)
			}
			var into *string
			err := q.QueryRow(ctx, `SELECT merged_into FROM parties WHERE id = $1`, id).Scan(&into)
			if errors.Is(err, store.ErrNotFound) {
				break // a key pointing at a party that no longer exists is not a candidate
			}
			if err != nil {
				return nil, err
			}
			if into == nil {
				break
			}
			id = *into
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// mergeParty records that one party was absorbed into another.
//
// The keys are deliberately left where they are. Moving them would make the
// absorbed party unresolvable and lose the fact that a source system knew this
// person under that identifier — and reads follow the merge pointer anyway, so
// there is nothing to gain by rewriting history to get the same answer.
func mergeParty(ctx context.Context, tx store.Querier, absorbed, survivor string, at time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE parties
		   SET merged_into = $2,
		       merged_at = $3,
		       doc = jsonb_set(jsonb_set(doc, '{mergedInto}', to_jsonb($2::text)),
		                       '{mergedAt}', to_jsonb($3::timestamptz))
		 WHERE id = $1 AND merged_into IS NULL`, absorbed, survivor, at)
	if err != nil {
		return err
	}
	if tag == 0 {
		return store.ErrNotFound
	}
	return nil
}

// mergedInto reports the survivor a party was absorbed into, if any.
func mergedInto(ctx context.Context, q store.Querier, partyID string) (*string, error) {
	var into *string
	err := q.QueryRow(ctx, `SELECT merged_into FROM parties WHERE id = $1`, partyID).Scan(&into)
	return into, err
}

// openHold reads one unresolved hold, locked for the transaction resolving it.
// Same reason the unclear queue locks its row: two custodians working the same
// queue is normal, and without the lock both can pass the "still open" check.
func openHold(ctx context.Context, tx store.Querier, holdID string) (Hold, error) {
	var h Hold
	err := tx.QueryRow(ctx, `
		SELECT id, key_kind, key_value, candidates, reason, created_at
		FROM match_holds WHERE id = $1 AND resolved_at IS NULL FOR UPDATE`, holdID).
		Scan(&h.ID, &h.KeyKind, &h.KeyValue, &h.Candidates, &h.Reason, &h.CreatedAt)
	return h, err
}

// markHoldResolved closes a hold, recording the decision, who took it, and —
// for a merge — who confirmed it and how. The confirmation columns are what
// turn "merges_without_confirmation = 0" into a query.
func markHoldResolved(ctx context.Context, tx store.Querier, holdID, decision,
	resolvedTo, resolvedBy, confirmedBy, method string, at time.Time) error {
	var confirmer, confirmMethod *string
	if confirmedBy != "" {
		confirmer, confirmMethod = &confirmedBy, &method
	}
	var to *string
	if resolvedTo != "" {
		to = &resolvedTo
	}
	_, err := tx.Exec(ctx, `
		UPDATE match_holds
		   SET resolved_at = $2, resolved_to = $3, resolved_by = $4,
		       decision = $5, confirmed_by = $6, confirmation_method = $7
		 WHERE id = $1 AND resolved_at IS NULL`,
		holdID, at, to, resolvedBy, decision, confirmer, confirmMethod)
	return err
}

// mergesWithoutConfirmation is the monitored invariant, as a query.
//
// It exists as code rather than as a note in a runbook because the number is
// meant to be checked by a test, and a metric nobody can compute is an
// aspiration (§4, W7).
func mergesWithoutConfirmation(ctx context.Context, q store.Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*) FROM match_holds
		 WHERE decision = 'merge' AND (confirmed_by IS NULL OR confirmation_method IS NULL)`).Scan(&n)
	return n, err
}

// removeKey takes one matching key off one party.
//
// Used when a hold is resolved `distinct`: the identifier belongs to one of the
// candidates, so it comes off the others. Nothing else about them changes —
// they are a different person, not a wrong record, and stripping the rest of
// their keys because they once shared a phone number would be the system
// punishing somebody for a household arrangement.
func removeKey(ctx context.Context, tx store.Querier, partyID, kind, value string) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM party_keys WHERE party_id = $1 AND key_kind = $2 AND key_value = $3`,
		partyID, kind, value)
	return err
}

// keyIdentitySubject is the party_keys kind a verified token resolves through.
//
// Deliberately absent from resolve()'s precedence list. That function answers
// "whose work is this?" from an identifier a source system supplied; this one
// answers "who is making this request?" from a token. A subject that could do
// both would let somebody's login attach a CSV row to them.
const keyIdentitySubject = "identity-subject"

// authenticating reports whether a binding's provider class is one somebody
// can present a token from. See migration 0008 for why the list is these three.
func authenticating(class schema.PartyIdentityBindingsItemProviderClass) bool {
	switch class {
	case schema.PartyIdentityBindingsItemProviderClassEsignet,
		schema.PartyIdentityBindingsItemProviderClassMosipIda,
		schema.PartyIdentityBindingsItemProviderClassGenericOidc:
		return true
	default:
		return false
	}
}

// partyForSubject answers which Party an authenticated subject is.
//
// Returns "" with no error when nobody is bound. That is an ordinary state,
// not a failure: somebody authenticated by the national system who has not been
// enrolled in this deployment is exactly who enrolment is for, and reporting it
// as an error would make every one of those requests look like a fault.
//
// Two parties on one subject cannot happen — 0008's unique index refuses it —
// and the query still refuses to choose if it somehow does, because a silent
// choice here is one person acting as another.
func partyForSubject(ctx context.Context, q store.Querier, subject string) (string, error) {
	rows, err := q.Query(ctx,
		`SELECT party_id FROM party_keys WHERE key_kind = $1 AND key_value = $2 LIMIT 2`,
		keyIdentitySubject, subject)
	if err != nil {
		return "", err
	}
	ids, err := store.Collect(rows, func(r store.Row) (string, error) {
		var v string
		return v, r.Scan(&v)
	})
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		if len(ids) > 1 {
			return "", fmt.Errorf("subject resolves to %d parties; refusing to choose", len(ids))
		}
		return "", nil
	}
	// Through any merge, so that somebody whose duplicate was closed keeps
	// logging in as themselves rather than as the record that was absorbed.
	survivors, err := followMerges(ctx, q, ids)
	if err != nil {
		return "", err
	}
	if len(survivors) != 1 {
		return "", nil
	}
	return survivors[0], nil
}
