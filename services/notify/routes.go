package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/id"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func routes(mux *http.ServeMux, d service.Deps) {
	h := &handlers{
		d:        d,
		registry: client.New(config.Str("REGISTRY_URL", "http://registry:8080")),
		sms:      client.New(config.Str("SMS_URL", "http://mock-sms:8080")),
	}
	mux.HandleFunc("POST /v1/notifications", h.send)
	mux.HandleFunc("GET /v1/notifications", h.list)
}

type handlers struct {
	d        service.Deps
	registry *client.Client
	sms      *client.Client
}

type notification struct {
	ID          string    `json:"id"`
	PartyID     string    `json:"partyId"`
	ClaimID     string    `json:"claimId,omitempty"`
	Kind        string    `json:"kind"`
	Channel     string    `json:"channel"`
	Destination string    `json:"destination"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	Failure     *string   `json:"failure,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (h *handlers) send(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PartyID string `json:"partyId"`
		ClaimID string `json:"claimId"`
		Kind    string `json:"kind"`
		// Subject is what the message is about, when it is not about a claim —
		// a source that stopped sending, say. Without it an operator is told
		// something is wrong and not which thing, which is an alert they
		// cannot act on. Unknown fields are rejected here rather than dropped,
		// so a caller with something to say has to have somewhere to say it.
		Subject  string    `json:"subject"`
		ClosesAt time.Time `json:"closesAt"`
	}
	if !httpx.ReadJSON(w, r, &req) {
		return
	}

	n := notification{
		ID:        id.New(h.d.Clock, "notification"),
		PartyID:   req.PartyID,
		ClaimID:   req.ClaimID,
		Kind:      req.Kind,
		CreatedAt: h.d.Clock.Now(),
	}

	route, err := h.routeFor(r.Context(), req.PartyID)
	switch {
	case err != nil:
		httpx.Fail(w, h.d.Log, "look up contact route", err)
		return
	case route == nil:
		// A worker with no reachable route is not an error to swallow. The row
		// records that they could not be told, which is what makes the
		// supervisor-assisted route something a person can be prompted to do
		// rather than something nobody knows is needed (§9).
		n.State = "UNREACHABLE"
		n.Channel = "none"
		n.Destination = ""
		n.Body = "no reachable contact route"
	default:
		n.Channel = string(route.Kind)
		n.Destination = route.Value
		n.Body = body(req.Kind, req.Subject, req.ClosesAt)
		if err := h.deliver(r.Context(), *route, n.Body); err != nil {
			msg := err.Error()
			n.State, n.Failure = "FAILED", &msg
		} else {
			n.State = "SENT"
		}
	}

	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		return insert(r.Context(), tx, n)
	}); err != nil {
		httpx.Fail(w, h.d.Log, "record notification", err)
		return
	}
	// A failed send is still a 201: the attempt is recorded, and returning an
	// error would have the relay redeliver and write the row again.
	httpx.WriteJSON(w, http.StatusCreated, n)
}

// routeFor picks where to reach a worker. A supervisor route is a route: for a
// worker with no phone, being told through their supervisor is the assisted
// path, not a failure.
func (h *handlers) routeFor(ctx context.Context, partyID string) (*schema.PartyContactRoutesItem, error) {
	var p schema.Party
	if err := h.registry.Get(ctx, "/v1/parties/"+url.PathEscape(partyID), &p); err != nil {
		if client.Code(err) == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	preference := []schema.PartyContactRoutesItemKind{
		schema.PartyContactRoutesItemKindPhone,
		schema.PartyContactRoutesItemKindUSSD,
		schema.PartyContactRoutesItemKindEmail,
		schema.PartyContactRoutesItemKindSupervisor,
	}
	for _, want := range preference {
		for i := range p.ContactRoutes {
			if p.ContactRoutes[i].Kind == want {
				return &p.ContactRoutes[i], nil
			}
		}
	}
	return nil, nil
}

func (h *handlers) deliver(ctx context.Context, route schema.PartyContactRoutesItem, text string) error {
	switch route.Kind {
	case schema.PartyContactRoutesItemKindPhone, schema.PartyContactRoutesItemKindUSSD:
		return h.sms.Post(ctx, "/send", map[string]any{"to": route.Value, "body": text}, nil)
	case schema.PartyContactRoutesItemKindSupervisor:
		// The supervisor is a Party, so this is one hop: tell them instead.
		// Recorded as its own channel so "who was actually told" stays honest.
		return h.sms.Post(ctx, "/send", map[string]any{
			"to": route.Value, "body": "For a worker you supervise: " + text}, nil)
	default:
		return nil // email is recorded, not sent, in a local stack
	}
}

// body is the worker-facing wording. It says what was recorded, by when they
// can object, and that they will be paid either way — because the alternative
// is a message that reads like a threat.
func body(kind, subject string, closesAt time.Time) string {
	switch kind {
	case "confirm-your-work":
		return fmt.Sprintf(
			"We have a record of work you did. Reply YES if it is right, or NO if it is not. "+
				"If you do not reply by %s we will accept it as recorded. You will be paid either way.",
			closesAt.Format("2 Jan"))
	case "source-went-quiet":
		// Addressed to an operator rather than a worker, and worded so it says
		// what to do. "Source X is unhealthy" tells somebody who already knows
		// the system; this tells somebody who has to go and ask a question —
		// and names the feed, because an alert that does not say which thing
		// broke is one nobody can act on.
		return fmt.Sprintf(
			"%s has stopped sending us work records. Work done since then is not being recorded. "+
				"Please check that feed.", subjectOr(subject, "A system that sends us work records"))
	default:
		return "There is an update about your work record."
	}
}

// subjectOr keeps the message readable when there is nothing to name.
func subjectOr(subject, fallback string) string {
	if subject == "" {
		return fallback
	}
	return subject
}

func insert(ctx context.Context, tx store.Querier, n notification) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO notifications (id, party_id, claim_id, kind, channel, destination, body,
		                           state, failure, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (party_id, claim_id, kind) DO NOTHING`,
		n.ID, n.PartyID, nullable(n.ClaimID), n.Kind, n.Channel, n.Destination,
		n.Body, n.State, n.Failure, n.CreatedAt)
	return err
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.d.DB.Q().Query(r.Context(), `
		SELECT id, party_id, coalesce(claim_id, ''), kind, channel, destination, body,
		       state, failure, created_at
		FROM notifications
		WHERE ($1 = '' OR party_id = $1)
		ORDER BY created_at, id`, r.URL.Query().Get("partyId"))
	if err != nil {
		httpx.Fail(w, h.d.Log, "list notifications", err)
		return
	}
	defer rows.Close()
	out, err := store.Collect(rows, func(row store.Row) (notification, error) {
		var n notification
		return n, row.Scan(&n.ID, &n.PartyID, &n.ClaimID, &n.Kind, &n.Channel, &n.Destination,
			&n.Body, &n.State, &n.Failure, &n.CreatedAt)
	})
	if err != nil {
		httpx.Fail(w, h.d.Log, "read notifications", err)
		return
	}
	if out == nil {
		out = []notification{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"notifications": out})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
