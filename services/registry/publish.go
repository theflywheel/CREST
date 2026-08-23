package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

// Publication of the registry's public facts (#20, Blueprint §3).
//
// The placement rule §3 states is not advice: public facts to the DeDi node,
// personal data to the private in-country store, never the reverse. Until this
// file existed, organisations, terms and authorizations lived only in Postgres,
// which meant a verifier could establish which organisation held which terms at
// issuance time only by asking CREST and believing the answer.
//
// A DeDi record is append-only. Everything below therefore treats "what reaches
// the node" as a decision to be made explicitly, one field at a time, because
// it is the one decision that cannot be undone.

// The three registries §3 names. Workers are conspicuously not among them.
const (
	registryOrganisations  = "organisations"
	registryTerms          = "terms"
	registryAuthorizations = "authorizations"
)

const topicPublishFact = "dedi.publish.fact"

// factMessage names what to publish, not what to publish it as. The projection
// is read from the database at delivery time so a redelivery publishes the
// current row rather than a stale copy carried in the queue.
type factMessage struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
}

func enqueueFact(ctx context.Context, tx store.Querier, kind, id string, version int) error {
	if version == 0 {
		version = 1
	}
	return store.Enqueue(ctx, tx, topicPublishFact, factMessage{Kind: kind, ID: id, Version: version})
}

// ErrNotPublishable is a fact that must not leave the country store.
//
// It is a refusal rather than a filter. A projection that quietly dropped a
// person and reported success would leave the caller believing a fact was
// published when nothing was, and the mistake would only surface as a verifier
// unable to resolve something they were told existed.
var ErrNotPublishable = errors.New("this fact holds personal data and must not reach the registry")

// organisationFace is the projection for an organisation, and it is an
// allow-list of four fields.
//
// contactRoutes and identityBindings are absent deliberately, and their absence
// is the point. An organisation's Party carries the same contact structure a
// person's does — a phone number, an email — and identityBindings carries
// salted national-id hashes. None of that belongs in an append-only public log
// even for an organisation, and a projection built by marshalling the Party
// would have published all of it.
func organisationFace(p schema.Party) (map[string]any, error) {
	if p.Kind != schema.PartyKindOrganisation {
		return nil, fmt.Errorf("%w: party %s is a %s", ErrNotPublishable, p.ID, p.Kind)
	}
	return map[string]any{
		"partyId":     p.ID,
		"kind":        string(p.Kind),
		"displayName": p.DisplayName,
		"createdAt":   p.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// termsFace publishes a Terms record whole, because a Terms record is public in
// its entirety: it is the document an organisation agreed to, and a verifier
// checking "under what terms was this work authorised" needs all of it.
func termsFace(t schema.Terms) map[string]any {
	out := map[string]any{
		"termsId":     t.ID,
		"name":        t.Name,
		"version":     t.Version,
		"permissions": t.Permissions,
		"publishedAt": t.PublishedAt.UTC().Format(time.RFC3339),
	}
	if t.Supersedes != nil {
		out["supersedes"] = t.Supersedes
	}
	return out
}

// authorizationFace publishes an authorization — but only one whose subject is
// an organisation.
//
// DESIGN FINDING. Blueprint §3 lists authorizations as public facts without
// qualifying which ones, and that cannot be right as written. A worker's
// authorization names a Party, a project context and a set of functions; a log
// of those is a public roster of who works where, permanently, for a population
// whose safety sometimes depends on that not being knowable. The pairwise party
// identifier does not fix it — a verifier who can see one credential can join
// it to the roster.
//
// So this refuses a person's authorization rather than publishing it, and the
// blueprint needs correcting to say "authorizations held by organisations". The
// consequence is real and is not being hidden: a verifier can check that an
// organisation was authorised, and cannot independently check that a particular
// worker was. Filed rather than worked around.
func authorizationFace(a schema.Authorization, subject schema.Party) (map[string]any, error) {
	if subject.Kind != schema.PartyKindOrganisation {
		return nil, fmt.Errorf("%w: authorization %s is held by a %s", ErrNotPublishable, a.ID, subject.Kind)
	}
	out := map[string]any{
		"authorizationId":   a.ID,
		"partyId":           a.PartyID,
		"authorityPartyId":  a.AuthorityPartyID,
		"approvedByPartyId": a.ApprovedByPartyID,
		"functions":         a.Functions,
		"scope":             a.Scope,
		"period":            a.Period,
		"state":             string(a.State),
		"terms":             a.Terms,
		"approvedAt":        a.ApprovedAt.UTC().Format(time.RFC3339),
	}
	if a.RevokedAt != nil {
		// A revocation that is not published is a revocation a verifier cannot
		// see, which is worse than never having published the grant.
		out["revokedAt"] = a.RevokedAt.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// registryFor maps a fact kind onto the registry it belongs in.
func registryFor(kind string) (string, error) {
	switch kind {
	case "organisation":
		return registryOrganisations, nil
	case "terms":
		return registryTerms, nil
	case "authorization":
		return registryAuthorizations, nil
	default:
		return "", fmt.Errorf("no registry for fact kind %q", kind)
	}
}

// deliver publishes one public fact.
//
// Idempotent on (kind, id, version), because the relay is at-least-once and a
// second identical version in an append-only log destroys the answer to "which
// version was in force when this credential was issued".
func deliver(d service.Deps) store.Deliverer {
	return func(ctx context.Context, topic string, payload json.RawMessage) error {
		if topic != topicPublishFact {
			return fmt.Errorf("no delivery route for topic %q", topic)
		}
		var msg factMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			return err
		}
		if d.DeDi == nil {
			return errors.New("a public fact needs publishing and this service has no registry substrate")
		}
		registry, err := registryFor(msg.Kind)
		if err != nil {
			return err
		}

		if _, err := publicationOf(ctx, d.DB.Q(), msg.Kind, msg.ID, msg.Version); err == nil {
			return nil // already published; a redelivery must not append a second version
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		face, err := projectFact(ctx, d, msg)
		switch {
		case errors.Is(err, ErrNotPublishable):
			// Not retried. This is not a transient failure — the fact will
			// never become publishable — and a queue that retries it forever
			// buries the messages that do need attention.
			d.Log.Warn("refusing to publish a fact that holds personal data",
				"kind", msg.Kind, "id", msg.ID, "error", err)
			return nil
		case err != nil:
			return err
		}

		ref := dedi.Ref{Namespace: d.DeDiNamespace, Registry: registry, Record: msg.ID}
		pre := dedi.Create()
		current, err := d.DeDi.Resolve(ctx, ref, false)
		switch {
		case err == nil:
			pre = dedi.Replace(current.Receipt.Tag())
		case errors.Is(err, dedi.ErrNotFound):
		default:
			return fmt.Errorf("read current registry state for %s: %w", ref, err)
		}

		receipt, err := d.DeDi.Publish(ctx, ref, face, pre)
		if err != nil {
			return fmt.Errorf("publish %s %s: %w", msg.Kind, msg.ID, err)
		}
		if !receipt.Transparent {
			d.Log.Warn("public fact published without a transparency proof",
				"kind", msg.Kind, "id", msg.ID,
				"consequence", "a verifier can check this only by trusting this deployment")
		}
		return recordPublication(ctx, d.DB, msg, receipt, d.Clock.Now())
	}
}

// projectFact reads the row and turns it into the document that goes to the node.
func projectFact(ctx context.Context, d service.Deps, msg factMessage) (map[string]any, error) {
	switch msg.Kind {
	case "organisation":
		p, err := getParty(ctx, d.DB.Q(), msg.ID)
		if err != nil {
			return nil, err
		}
		return organisationFace(p)
	case "terms":
		t, err := getTerms(ctx, d.DB.Q(), msg.ID, msg.Version)
		if err != nil {
			return nil, err
		}
		return termsFace(t), nil
	case "authorization":
		a, err := getAuthorization(ctx, d.DB.Q(), msg.ID)
		if err != nil {
			return nil, err
		}
		subject, err := getParty(ctx, d.DB.Q(), a.PartyID)
		if err != nil {
			// A subject this service cannot read is a subject it cannot prove
			// is an organisation, and the safe reading of "cannot prove" is
			// "do not publish".
			return nil, fmt.Errorf("%w: cannot read the subject of authorization %s: %w",
				ErrNotPublishable, a.ID, err)
		}
		return authorizationFace(a, subject)
	default:
		return nil, fmt.Errorf("no projection for fact kind %q", msg.Kind)
	}
}
