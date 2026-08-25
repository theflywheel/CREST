package harness

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/theflywheel/crest/harness/fixtures"
	"github.com/theflywheel/crest/pkg/pii"
	"github.com/theflywheel/crest/pkg/schema"
)

// Seed loads the canonical fixture world into a running stack, through the
// same HTTP endpoints a real deployment is set up through.
//
// Through the real endpoints on purpose. A seeder that writes rows directly
// would let the world contain things the services would have refused — a
// self-ratified definition, an authorization with no approver — and every test
// built on it would then agree with a bug.
func (s *Stack) Seed(ctx context.Context) (*fixtures.World, error) {
	w, err := fixtures.Load()
	if err != nil {
		return nil, err
	}

	if err := s.SetClock(ctx, w.Instance.Epoch); err != nil {
		return nil, fmt.Errorf("set clock: %w", err)
	}

	for _, p := range w.Parties {
		if err := s.Parties.Post(ctx, "/v1/parties", p, nil); err != nil {
			return nil, fmt.Errorf("party %s: %w", p.DisplayName, err)
		}
	}
	// Skills before definitions: a definition names a skill code, and a
	// deployment that has not adopted the taxonomy would be publishing a
	// definition pointing at a vocabulary entry nobody can resolve.
	for _, sk := range w.Skills {
		// A skill code already published is the normal case on a stack that is
		// already up, and refusing it is right — the code is immutable — so
		// the seeder skips rather than treating "it is already correct" as a
		// failure.
		if err := s.Parties.Get(ctx, "/v1/skills/"+url.PathEscape(sk.Code), nil); err == nil {
			continue
		}
		if err := s.Parties.Post(ctx, "/v1/skills", sk, nil); err != nil {
			return nil, fmt.Errorf("skill %s: %w", sk.Code, err)
		}
	}
	for _, t := range w.Terms {
		if err := s.Parties.Post(ctx, "/v1/terms", t, nil); err != nil {
			return nil, fmt.Errorf("terms %s: %w", t.Name, err)
		}
	}
	for _, c := range w.Contexts {
		if err := s.Parties.Post(ctx, "/v1/contexts", c, nil); err != nil {
			return nil, fmt.Errorf("context %s: %w", c.Name, err)
		}
	}
	for _, a := range w.Authorizations {
		if err := s.Parties.Post(ctx, "/v1/authorizations", a, nil); err != nil {
			return nil, fmt.Errorf("authorization %s: %w", a.ID, err)
		}
	}

	// The definition goes in as a DRAFT and is then ratified and activated, so
	// seeding exercises the same separation-of-duties path a real one takes
	// (§7). Seeding straight into ACTIVE would skip the only rule the
	// definitions service exists to enforce.
	//
	// Seeding is idempotent because scenarios run against one stack: a second
	// scenario finding the definition already ACTIVE is the normal case, not a
	// failure. What is *not* idempotent is creating the version twice — the
	// service refuses that, and it is right to, because a definition version is
	// immutable from the moment it exists.
	for _, d := range w.Definitions {
		var existing schema.Definition
		if err := s.Definitions.Get(ctx, "/v1/definitions/"+d.ID, &existing); err == nil &&
			existing.State == schema.DefinitionStateACTIVE {
			continue
		}
		ratifier := d.RatifiedByPartyID
		draft := d
		draft.State = schema.DefinitionStateDRAFT
		draft.RatifiedByPartyID = nil
		draft.ActivatedAt = nil
		if err := s.Definitions.Post(ctx, "/v1/definitions", draft, nil); err != nil {
			return nil, fmt.Errorf("definition %s: %w", d.ID, err)
		}
		if ratifier == nil {
			return nil, fmt.Errorf("definition %s has no ratifier in the fixture world", d.ID)
		}
		path := fmt.Sprintf("/v1/definitions/%s/versions/%d", d.ID, d.Version)
		if err := s.Definitions.Post(ctx, path+"/ratify",
			map[string]any{"ratifiedByPartyId": *ratifier}, nil); err != nil {
			return nil, fmt.Errorf("ratify %s: %w", d.ID, err)
		}
		if err := s.Definitions.Post(ctx, path+"/activate", nil, nil); err != nil {
			return nil, fmt.Errorf("activate %s: %w", d.ID, err)
		}
	}

	for _, lr := range w.LinkedRecords {
		if lr.KeyedTo.Kind != schema.LinkedRecordKeyedToKindDefinition {
			continue
		}
		if err := s.Definitions.Post(ctx,
			"/v1/definitions/"+lr.KeyedTo.ID+"/linked-records", lr, nil); err != nil {
			return nil, fmt.Errorf("linked record %s: %w", lr.ID, err)
		}
	}
	return w, nil
}

// PhoneOf returns a party's phone number, which is what a CSV joins on.
func PhoneOf(w *fixtures.World, partyID string) (string, error) {
	p, ok := w.Party(partyID)
	if !ok {
		return "", fmt.Errorf("no party %s", partyID)
	}
	for _, r := range p.ContactRoutes {
		if r.Kind == schema.PartyContactRoutesItemKindPhone {
			return r.Value, nil
		}
	}
	return "", fmt.Errorf("%s has no phone route", partyID)
}

// LocalHasher is the salt the local stack runs with. The harness needs it to
// build the same national-identifier hash the pipeline will compute — never to
// reverse one, which nothing can.
func LocalHasher() (*pii.Hasher, error) {
	return pii.NewHasher(
		env("NATIONAL_ID_SALT", "local-development-salt-not-for-any-real-deployment"),
		env("NATIONAL_ID_SALT_REF", "local-1"))
}

// Epoch is the fixture world's starting instant, for tests that need to reason
// about dates without loading the world.
func Epoch() time.Time { return time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC) }
