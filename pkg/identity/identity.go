// Package identity establishes who is making a request, rather than believing
// what the request says about itself.
//
// Until this existed, every CREST endpoint answered whoever could reach it.
// Authorization worked and asked the right question — "is party X permitted to
// do this here" — but X arrived in a query string, so a caller who named a
// party they were not was indistinguishable from one who was (#89). That is
// tolerable on a read. It is not tolerable on the three endpoints that act in
// somebody's name: withdrawing a worker's enrolment consent, approving an
// organisation, and the T=7 exits.
//
// The identity provider is the deployment's, not ours. §4.1 already names
// eSignet as the identity anchor, and inventing a second notion of who somebody
// is would be the layering test's own example of what not to do. So this
// package verifies an OIDC access token and nothing more: the issuer, its keys,
// and its audience are configuration.
//
// # The subject is re-salted here
//
// A provider's `sub` is a pairwise pseudonym, and eSignet's is partitioned by
// relying party rather than by client (#52) and is derived without a
// per-deployment salt (#63). Both findings are open against the substrate. So
// nothing in CREST stores or compares the provider's subject directly: it is
// put through HMAC-SHA256 under this deployment's own salt first, and it is
// that value a Party is bound to. Two deployments reading the same eSignet
// therefore cannot correlate their workers by subject, whatever eSignet does.
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Caller is an authenticated principal.
//
// A zero Caller is an unauthenticated request, which is a legitimate state — a
// verifier reading a public credential has no identity and needs none. What is
// not legitimate is a handler that acts in somebody's name without checking.
type Caller struct {
	// Subject is this deployment's pairwise reference for the principal, after
	// salting. Never the provider's own `sub`.
	Subject string

	// Issuer is the identity provider that authenticated them.
	Issuer string

	// PartyID is the CREST Party this subject is bound to, or empty. Empty is
	// ordinary: a person can be authenticated by the national system and not
	// yet exist in this deployment's registry, which is precisely the state
	// enrolment is for.
	PartyID string

	// ExpiresAt is when the token stops being evidence of anything.
	ExpiresAt time.Time

	// requestedFor is the party named in X-CREST-On-Behalf-Of. It is
	// unexported on purpose: a handler that could read it directly would read
	// a request as a decision, and the assisted path is exactly where that
	// mistake costs a worker their record. Acting is the only way out, and it
	// takes the permission check as an argument.
	requestedFor string

	// sameParty expands a party id across any merge (#100, #104). Set by the
	// middleware from the deployment's registry; nil in a service with no
	// identity provider, and in unit tests that construct a Caller directly.
	//
	// It is called lazily and only by Actor, only when the named party and the
	// proven one differ. That is deliberate on both counts: the answer costs a
	// registry round trip in every service but parties, and the case it exists
	// for — somebody's record was merged — is rare. A request that names the
	// party it proved, which is nearly all of them, never asks.
	sameParty SameFunc
}

// SameFunc answers which party ids are one person, following merges.
//
// The same shape the services already thread through service.Deps, so this
// package is given the registry's existing answer rather than inventing a
// second notion of who is who.
type SameFunc func(ctx context.Context, partyID string) ([]string, error)

// WithSameParty returns a copy of the caller that can expand ids across
// merges. The middleware wires it; handlers never need to.
func (c Caller) WithSameParty(f SameFunc) Caller {
	c.sameParty = f
	return c
}

// Authenticated reports whether a token was verified for this request.
func (c Caller) Authenticated() bool { return c.Subject != "" }

// Errors a handler is expected to branch on.
var (
	// ErrNoCaller means no verified token accompanied the request.
	ErrNoCaller = errors.New("identity: the request carries no verified caller")

	// ErrUnbound means the caller is authenticated but no Party is bound to
	// their subject, so there is nobody for them to be acting as.
	ErrUnbound = errors.New("identity: this subject is not bound to any party")

	// ErrNotPermitted means the caller asked to act for somebody else and
	// holds no authorization that allows it.
	ErrNotPermitted = errors.New("identity: this caller may not act for that party")

	// ErrImpersonation means the request named one party and proved another.
	// This is the failure #89 is about: before this package, it had no name
	// because there was nothing to compare the named party against.
	//
	// "Another" means another person, not another id. A person whose duplicate
	// record was closed has more than one id and both are theirs (#100, #104);
	// naming the absorbed one is not impersonation. See Actor.
	ErrImpersonation = errors.New("identity: the request names a party it has not proven")

	// ErrRegistryUnavailable means the registry could not say whether two ids
	// are the same person, so this request cannot be decided either way.
	//
	// Deliberately not folded into ErrImpersonation. Refusing a merged worker
	// with "you are impersonating somebody" because a lookup timed out is a
	// false accusation in a log an operator will later read, and it points the
	// investigation at the worker instead of at the outage.
	ErrRegistryUnavailable = errors.New("identity: the registry could not say which ids are this party")
)

// PermitsFunc answers whether a party may perform a function in a context. It
// is the shape of the registry's existing authorization check, deliberately —
// this package adds authentication and does not invent a second authorization
// model beside the one §4 already describes.
type PermitsFunc func(ctx context.Context, partyID, function, contextID string) (bool, error)

// FunctionActForParty is the authorization an assisted action requires.
//
// The assisted case is not an attack to be closed off. A worker with no phone
// confirming through a supervisor is one of the four T=7 exits, and a
// registering agent enrolling someone in a field visit is how most workers get
// into the system at all. What makes it safe is that it is recorded as itself:
// somebody's name is on the assistance, and that somebody had to be permitted.
const FunctionActForParty = "act-for-party"

// Acting answers which party this request acts on.
//
// Unassisted, that is the caller's own party. Assisted, it is the party they
// named — but only if they hold FunctionActForParty in the context, and the
// caller is returned to the handler having been checked rather than assumed.
//
// contextID may be empty for an action that is not scoped to a project; the
// authorization must then be one granted without a scope.
func Acting(ctx context.Context, c Caller, contextID string, permits PermitsFunc) (string, error) {
	if !c.Authenticated() {
		return "", ErrNoCaller
	}
	if c.requestedFor == "" || c.requestedFor == c.PartyID {
		if c.PartyID == "" {
			return "", ErrUnbound
		}
		return c.PartyID, nil
	}
	// Acting for somebody else requires being somebody first. An unbound
	// caller assisting a worker would leave assistance with no name on it,
	// and "every held payment has a reason with an owner" has an equivalent
	// here: every assisted action has an assistant with an identity.
	if c.PartyID == "" {
		return "", ErrUnbound
	}
	ok, err := permits(ctx, c.PartyID, FunctionActForParty, contextID)
	if err != nil {
		return "", fmt.Errorf("checking whether %s may act for %s: %w", c.PartyID, c.requestedFor, err)
	}
	if !ok {
		return "", fmt.Errorf("%w: %s for %s", ErrNotPermitted, c.PartyID, c.requestedFor)
	}
	return c.requestedFor, nil
}

// Assisting reports whether this request is one party acting for another. It
// is what a handler records so the action is stored as assisted rather than as
// the worker's own — the difference between a supervisor-assisted confirmation
// and a forged one is entirely in whether anybody wrote it down.
func (c Caller) Assisting() bool {
	return c.requestedFor != "" && c.requestedFor != c.PartyID
}

// RequestedFor exposes the named party for logging and for the assisted-route
// record. It says what was asked for, never what was allowed — read Acting's
// return value for that.
func (c Caller) RequestedFor() string { return c.requestedFor }

// Actor resolves the party a request acts as, refusing one it cannot prove.
//
// `claimed` is the party id the request body or query named, which is how every
// CREST endpoint used to decide this on its own. It is now a cross-check rather
// than an answer: if it disagrees with what the caller proved, the request is
// refused instead of believed.
//
// `enforced` is whether this deployment has an identity provider at all. With
// one, an unauthenticated request to an endpoint that acts in somebody's name
// is refused. Without one — a `go run` of a single service, a unit test — the
// claimed party is returned and the service has already said loudly at start-up
// that its callers are not authenticated. pkg/service refuses to start a
// production deployment in that state, which is what keeps this branch out of
// anywhere it would matter.
//
// # A person is not one id (#216)
//
// The cross-check was string equality until #216, and that made a merged
// worker's own history unreachable. A person whose duplicate record was closed
// keeps both ids: parties' binder deliberately follows merges, so the id this
// request PROVES is always the survivor, while the id it NAMES may be the
// absorbed one — a verifier's saved bookmark, a link in a message sent last
// month, a worker's own device that enrolled before the merge. Comparing the
// two as strings called that impersonation and refused, three lines above the
// handler that would have expanded the ids correctly.
//
// So the comparison asks the registry the same question the handlers ask it:
// are these ids one person? If they are, the request stands and the SURVIVOR
// is returned — the caller acts as themselves under their current id, and a
// handler that goes on to expand it gets the whole chain. §16 and #104 rule
// that continuity wins and the merge is not disclosed; returning the survivor
// keeps the absorbed id working without the response ever saying why.
//
// This does not widen anything. An id that is genuinely somebody else's is not
// in the chain, so it is still refused, and the assisted route through
// X-CREST-On-Behalf-Of and FunctionActForParty is still the only way to act
// for another person.
func Actor(ctx context.Context, c Caller, claimed, contextID string, enforced bool, permits PermitsFunc) (string, error) {
	if !c.Authenticated() {
		return "", ErrNoCaller
	}
	proven, err := Acting(ctx, c, contextID, permits)
	if err != nil {
		return "", err
	}
	if claimed == "" || claimed == proven {
		return proven, nil
	}
	same, err := c.samePartyAs(ctx, proven, claimed)
	if err != nil {
		return "", err
	}
	if !same {
		return "", fmt.Errorf("%w: named %s, proved %s", ErrImpersonation, claimed, proven)
	}
	return proven, nil
}

// samePartyAs reports whether `claimed` is another id for the person who
// proved `proven`.
//
// With no expander wired — a service with no identity provider, or a unit test
// building a Caller by hand — this is the old string comparison, which has
// already failed by the time it is called. That is the safe direction: a
// deployment that cannot ask the registry refuses rather than accepts.
func (c Caller) samePartyAs(ctx context.Context, proven, claimed string) (bool, error) {
	if c.sameParty == nil {
		return false, nil
	}
	ids, err := c.sameParty(ctx, proven)
	if err != nil {
		return false, fmt.Errorf("%w: %s: %w", ErrRegistryUnavailable, proven, err)
	}
	for _, id := range ids {
		if id == claimed {
			return true, nil
		}
	}
	return false, nil
}

type ctxKey struct{}

// NewContext carries a Caller through a request.
func NewContext(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// From reads the Caller a request was authenticated as. The zero value means
// the request was unauthenticated, which callers must treat as a decision
// rather than as a default.
func From(ctx context.Context) Caller {
	c, _ := ctx.Value(ctxKey{}).(Caller)
	return c
}

// Pairwise is this deployment's reference for a provider subject.
//
// HMAC rather than a bare hash of salt+input: the salt is a key here, not a
// nonce, and a bare hash of a secret prefix is the length-extension mistake
// that has been made in this exact position often enough to be worth avoiding
// by construction.
//
// The issuer is inside the input so that two providers cannot produce the same
// CREST subject by issuing the same `sub` — which they will, because "1234" is
// a subject somebody has issued.
func Pairwise(salt []byte, issuer, sub string) string {
	m := hmac.New(sha256.New, salt)
	// Length-prefixed rather than concatenated: iss="a" sub="bc" and iss="ab"
	// sub="c" must not collide, and with a separator alone they still can when
	// a subject contains the separator.
	// hash.Hash never returns an error from Write, which is why this is the
	// one place a discarded return is honest rather than lazy.
	_, _ = fmt.Fprintf(m, "%d:%s%d:%s", len(issuer), issuer, len(sub), sub)
	return hex.EncodeToString(m.Sum(nil))
}

// Denial maps an identity failure to the status a caller should see.
//
// Three outcomes and they are genuinely different. 401 says "you have not told
// me who you are"; 403 with unbound says "I know who you are and this
// deployment has never heard of you", which is what an unenrolled worker gets
// and is a prompt to enrol rather than a refusal; 403 with not-permitted says
// "I know who you are and you may not do this for them". Collapsing them into
// one status is how somebody spends an afternoon on the wrong problem.
func Denial(err error) (status int, code, detail string, ok bool) {
	switch {
	case errors.Is(err, ErrNoCaller):
		return 401, "unauthenticated",
			"this endpoint acts in somebody's name and needs a verified caller", true
	case errors.Is(err, ErrUnbound):
		return 403, "subject_not_enrolled",
			"you are authenticated, but no party in this deployment is bound to your identity", true
	case errors.Is(err, ErrImpersonation):
		return 403, "party_not_proven",
			"this request names a party other than the one it authenticated as; " +
				"to act for somebody else, send " + HeaderOnBehalfOf + " and hold the authorization for it", true
	case errors.Is(err, ErrNotPermitted):
		return 403, "not_permitted_to_act_for",
			"acting for another party needs the " + FunctionActForParty + " authorization in this context", true
	case errors.Is(err, ErrRegistryUnavailable):
		// 503, and the fourth genuinely different outcome. The registry could
		// not say whether the named id is another id for this same person, so
		// nothing about the caller was rejected — the question was never
		// answered. A 403 here would tell a merged worker they are somebody
		// else because a lookup timed out.
		return 503, "registry_unavailable",
			"the registry could not say which ids are this party, so this request cannot be decided; " +
				"this is an outage, not a refusal", true
	}
	return 0, "", "", false
}
