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
	ErrImpersonation = errors.New("identity: the request names a party it has not proven")
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
func Actor(ctx context.Context, c Caller, claimed, contextID string, enforced bool, permits PermitsFunc) (string, error) {
	if !c.Authenticated() {
		if enforced {
			return "", ErrNoCaller
		}
		return claimed, nil
	}
	proven, err := Acting(ctx, c, contextID, permits)
	if err != nil {
		return "", err
	}
	if claimed != "" && claimed != proven {
		return "", fmt.Errorf("%w: named %s, proved %s", ErrImpersonation, claimed, proven)
	}
	return proven, nil
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
	}
	return 0, "", "", false
}
