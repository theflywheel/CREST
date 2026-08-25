package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// Appending an identity binding (#17, §4.1).
//
// The Party schema has always said `identityBindings` is append-only — "re-binding
// adds a record; history is never rewritten" — and until now there was no way to
// add one after the Party was created. A schema that promises append-only with
// no append is a promise nothing can keep, and the cost fell on exactly the
// person this system is for: a worker enrolled by phone in a field visit was
// stuck at IA-1 forever, because the only moment their assurance could ever be
// established was the moment somebody else typed them into a form.
//
// This is the production trigger for the retroactive upgrade. Assurance is
// derived from bindings and strength reads assurance, so appending one raises
// the tier of evidence already captured, with nothing reissued and no stored
// judgement to migrate.

// ErrBindingBelongsToAnother is returned when the identifier being bound
// already resolves to a different Party.
var ErrBindingBelongsToAnother = errors.New("that identifier already belongs to another party")

// appendBinding adds a binding without disturbing the ones already there.
//
// It never edits and never deletes. A superseded binding stays exactly where it
// is: it is the true statement that this is who we thought they were then, and
// it is what makes a later dispute about an attribution answerable at all.
func appendBinding(ctx context.Context, tx store.Querier, partyID string,
	b schema.PartyIdentityBindingsItem) (schema.Party, bool, error) {

	p, err := getParty(ctx, tx, partyID)
	if err != nil {
		return schema.Party{}, false, err
	}

	// Exactly this binding, already recorded. A retry of the same request must
	// not grow the history — but note how narrow this is: the same provider
	// with a *different* subject is a genuine re-binding and appends, because
	// that is the case where somebody's anchor actually changed.
	for _, existing := range p.IdentityBindings {
		if existing.Provider == b.Provider &&
			existing.ProviderClass == b.ProviderClass &&
			existing.SubjectRef == b.SubjectRef {
			return p, false, nil
		}
	}

	// A subject reaching a second Party would mean one login authenticating as
	// two people, and the service picking one. Refused for the same reason the
	// national identifier below is, and more sharply: this one is not a
	// question about whose work a row is, it is a question about who is
	// holding the phone, and there is no queue that can resolve it later.
	if authenticating(b.ProviderClass) && b.SubjectRef != "" {
		owner, err := ownerOfKey(ctx, tx, keyIdentitySubject, b.SubjectRef)
		if err != nil {
			return schema.Party{}, false, err
		}
		if owner != "" && owner != partyID {
			return schema.Party{}, false, fmt.Errorf("%w: %s", ErrBindingBelongsToAnother, owner)
		}
	}

	// A national identifier reaching a second Party is not a re-binding, it is
	// two records for one person — the duplicate case, and the rule is that it
	// holds rather than merging. Refusing here is the conservative half of that:
	// binding it would silently attach this worker's history to an identifier
	// somebody else is already known by.
	if b.NationalIDHash != nil {
		owner, err := ownerOfKey(ctx, tx, "national-id-hash", b.NationalIDHash.Value)
		if err != nil {
			return schema.Party{}, false, err
		}
		if owner != "" && owner != partyID {
			return schema.Party{}, false, fmt.Errorf("%w: %s", ErrBindingBelongsToAnother, owner)
		}
	}

	p.IdentityBindings = append(p.IdentityBindings, b)
	if err := schema.Validate(schema.IDParty, p); err != nil {
		return schema.Party{}, false, err
	}
	if err := insertParty(ctx, tx, p); err != nil {
		return schema.Party{}, false, err
	}
	return p, true, nil
}

// ownerOfKey answers which party a key value belongs to, if exactly one does.
func ownerOfKey(ctx context.Context, q store.Querier, kind, value string) (string, error) {
	rows, err := q.Query(ctx,
		`SELECT party_id FROM party_keys WHERE key_kind = $1 AND key_value = $2 LIMIT 2`, kind, value)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var owners []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return "", err
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(owners) == 1 {
		return owners[0], nil
	}
	return "", nil
}

func (h *handlers) addIdentityBinding(w http.ResponseWriter, r *http.Request) {
	var b schema.PartyIdentityBindingsItem
	if !httpx.ReadJSON(w, r, &b) {
		return
	}
	// Appending a binding raises assurance, and assurance raises the tier of
	// evidence already captured — so anyone who could append to anyone could
	// upgrade anyone (#102). Two legitimate callers: somebody permitted to act
	// for the party (an enrolment agent), or the subject themselves — proven
	// not by naming the party but by HOLDING the token whose subject is being
	// bound, which is the one proof that needs no prior binding and so does
	// not chicken-and-egg first login.
	if h.d.Authenticating {
		caller := identity.From(r.Context())
		selfProof := caller.Authenticated() && b.SubjectRef != "" && caller.Subject == b.SubjectRef
		if !selfProof {
			// The agent's act-for-party grant is ordinarily context-scoped
			// ("an instance-wide grant to act for anybody is a grant to be
			// anybody" — fixtures), so the enrolment context rides as a query
			// parameter for the authorization check. It scopes the CHECK, not
			// the binding: a binding has no context.
			if _, ok := identity.Authorize(w, r, h.d.Log, r.PathValue("id"),
				r.URL.Query().Get("contextId"), h.d.Authenticating, h.d.Permits); !ok {
				return
			}
		}
	}
	if b.AssertedAt.IsZero() {
		b.AssertedAt = h.d.Clock.Now()
	}
	// An assertion dated in the future is either a clock problem or an attempt
	// to hold an assurance level open past its expiry. Neither should be stored.
	if b.AssertedAt.After(h.d.Clock.Now().Add(time.Minute)) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
			"a binding cannot have been asserted in the future")
		return
	}

	partyID := r.PathValue("id")
	var (
		party    schema.Party
		appended bool
	)
	err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		var err error
		party, appended, err = appendBinding(r.Context(), tx, partyID, b)
		return err
	})
	if err == nil && appended && h.d.ForgetSubject != nil && b.SubjectRef != "" {
		// The bind request itself primed the middleware's cache with "nobody"
		// on the way in; drop that so the very next request from this subject
		// is somebody (#102).
		h.d.ForgetSubject(b.SubjectRef)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "no such party")
		return
	case errors.Is(err, ErrBindingBelongsToAnother):
		// 409, not 400. The request is well formed and the caller is not
		// necessarily wrong — this is the duplicate queue's problem, and it is
		// resolved by a person, never by whoever happened to write last.
		httpx.WriteError(w, http.StatusConflict, "identifier_belongs_to_another_party",
			"that identifier already resolves to a different party; this holds for review rather than merging")
		return
	case err != nil:
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			writeValidation(w, err)
			return
		}
		httpx.Fail(w, h.d.Log, "append identity binding", err)
		return
	}

	// The assurance is returned because it is the thing the caller actually
	// wanted to change, and computing it here saves a second request that could
	// otherwise be made against a stale read.
	level, because := assuranceOf(party, h.d.Clock.Now())
	status := http.StatusCreated
	if !appended {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, map[string]any{
		"partyId":           party.ID,
		"bindings":          party.IdentityBindings,
		"identityAssurance": level,
		"because":           because,
	})
}
