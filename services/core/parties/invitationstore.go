package parties

import (
	"context"
	"encoding/json"

	"github.com/theflywheel/crest/pkg/store"
)

// Storage for invitations and terms requests. The document is the record and
// the columns are an index, as everywhere else in this service.

func upsertInvitation(ctx context.Context, tx store.Querier, inv invitation) error {
	doc, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO project_invitations (id, context_id, party_id, doc, state, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET doc = EXCLUDED.doc, state = EXCLUDED.state`,
		inv.ID, inv.ContextID, inv.PartyID, doc, inv.State, inv.InvitedAt)
	return err
}

func getInvitation(ctx context.Context, q store.Querier, id string, forUpdate bool) (invitation, error) {
	sql := `SELECT doc FROM project_invitations WHERE id = $1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	var doc []byte
	if err := q.QueryRow(ctx, sql, id).Scan(&doc); err != nil {
		return invitation{}, err
	}
	var inv invitation
	return inv, json.Unmarshal(doc, &inv)
}

// listInvitations answers one side's stake: the project's offers, or the
// organisation's inbox. Never both empty — an unnarrowed listing would tell
// any caller which organisations every project is courting.
func listInvitations(ctx context.Context, q store.Querier, contextID, partyID, state string) ([]invitation, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM project_invitations
		WHERE ($1 = '' OR context_id = $1)
		  AND ($2 = '' OR party_id = $2)
		  AND ($3 = '' OR state = $3)
		ORDER BY created_at, id`, contextID, partyID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (invitation, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return invitation{}, err
		}
		var inv invitation
		return inv, json.Unmarshal(doc, &inv)
	})
}

// appendInvitationEvent writes one trail row, sequenced inside the caller's
// transaction so two answers cannot race into the same seq.
func appendInvitationEvent(ctx context.Context, tx store.Querier, invitationID string, ev invitationEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO invitation_events (invitation_id, seq, event, actor_party_id, note, at)
		VALUES ($1,
		        (SELECT coalesce(max(seq), 0) + 1 FROM invitation_events WHERE invitation_id = $1),
		        $2, $3, $4, $5)`,
		invitationID, ev.Event, ev.ActorPartyID, ev.Note, ev.At)
	return err
}

func invitationEvents(ctx context.Context, q store.Querier, invitationID string) ([]invitationEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT seq, event, actor_party_id, note, at
		FROM invitation_events WHERE invitation_id = $1 ORDER BY seq`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (invitationEvent, error) {
		var e invitationEvent
		return e, r.Scan(&e.Seq, &e.Event, &e.ActorPartyID, &e.Note, &e.At)
	})
}

func upsertTermsRequest(ctx context.Context, tx store.Querier, req termsRequest) error {
	doc, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO terms_requests (id, party_id, doc, state, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET doc = EXCLUDED.doc, state = EXCLUDED.state`,
		req.ID, req.PartyID, doc, req.State, req.CreatedAt)
	return err
}

func getTermsRequest(ctx context.Context, q store.Querier, id string, forUpdate bool) (termsRequest, error) {
	sql := `SELECT doc FROM terms_requests WHERE id = $1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	var doc []byte
	if err := q.QueryRow(ctx, sql, id).Scan(&doc); err != nil {
		return termsRequest{}, err
	}
	var req termsRequest
	return req, json.Unmarshal(doc, &req)
}

func listTermsRequests(ctx context.Context, q store.Querier, partyID, state string) ([]termsRequest, error) {
	rows, err := q.Query(ctx, `
		SELECT doc FROM terms_requests
		WHERE ($1 = '' OR party_id = $1)
		  AND ($2 = '' OR state = $2)
		ORDER BY created_at, id`, partyID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (termsRequest, error) {
		var doc []byte
		if err := r.Scan(&doc); err != nil {
			return termsRequest{}, err
		}
		var req termsRequest
		return req, json.Unmarshal(doc, &req)
	})
}

func appendTermsRequestEvent(ctx context.Context, tx store.Querier, requestID string, ev termsRequestEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO terms_request_events (request_id, seq, event, actor_party_id, reason, at)
		VALUES ($1,
		        (SELECT coalesce(max(seq), 0) + 1 FROM terms_request_events WHERE request_id = $1),
		        $2, $3, $4, $5)`,
		requestID, ev.Event, ev.ActorPartyID, ev.Reason, ev.At)
	return err
}

func termsRequestEvents(ctx context.Context, q store.Querier, requestID string) ([]termsRequestEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT seq, event, actor_party_id, reason, at
		FROM terms_request_events WHERE request_id = $1 ORDER BY seq`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (termsRequestEvent, error) {
		var e termsRequestEvent
		return e, r.Scan(&e.Seq, &e.Event, &e.ActorPartyID, &e.Reason, &e.At)
	})
}

func appendCheckVerdict(ctx context.Context, tx store.Querier, requestID string, v checkVerdict) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO terms_request_checks (request_id, seq, name, outcome, owner_kind, owner, note, recorded_by, at)
		VALUES ($1,
		        (SELECT coalesce(max(seq), 0) + 1 FROM terms_request_checks WHERE request_id = $1),
		        $2, $3, $4, $5, $6, $7, $8)`,
		requestID, v.Name, v.Outcome, v.OwnerKind, v.Owner, v.Note, v.RecordedBy, v.At)
	return err
}

func checkVerdicts(ctx context.Context, q store.Querier, requestID string) ([]checkVerdict, error) {
	rows, err := q.Query(ctx, `
		SELECT seq, name, outcome, owner_kind, owner, coalesce(note, ''), recorded_by, at
		FROM terms_request_checks WHERE request_id = $1 ORDER BY seq`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (checkVerdict, error) {
		var v checkVerdict
		return v, r.Scan(&v.Seq, &v.Name, &v.Outcome, &v.OwnerKind, &v.Owner, &v.Note, &v.RecordedBy, &v.At)
	})
}

// applyApprovedTerms moves an organisation's registration to the terms version
// an approved request asked for. A direct write on purpose: acceptTerms()
// refuses once a registration is decided, and that refusal is right for the
// unilateral path — an approved organisation must not re-choose its own terms.
// This is the reviewed path that upgrade was always going to need: the org
// asked (the request's submission is its acceptance of the wider terms), a
// named decider who is not the org approved, and only then does the held terms
// version move. accepted_by records the request's submitter — the acceptance
// was theirs, not the decider's.
func applyApprovedTerms(ctx context.Context, tx store.Querier, req termsRequest) error {
	affected, err := tx.Exec(ctx, `
		UPDATE org_registrations
		SET terms_id = $2, terms_version = $3, accepted_by = $4, accepted_at = $5
		WHERE party_id = $1 AND state = 'APPROVED'`,
		req.PartyID, req.TermsID, req.TermsVersion, req.SubmittedBy, req.DecidedAt)
	if err != nil {
		return err
	}
	if affected == 0 {
		return store.ErrNotFound
	}
	return nil
}
