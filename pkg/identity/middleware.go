package identity

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// HeaderOnBehalfOf is how a caller says they are acting for somebody else.
//
// A header rather than a body field, because it applies to every endpoint the
// assisted paths touch and a body field would have to be added to each one —
// which is the same as saying some of them would not get it.
const HeaderOnBehalfOf = "X-CREST-On-Behalf-Of"

// Binder answers which Party a verified subject is bound to.
//
// An interface because the registry answers it from its own table and the
// other six services have to ask the registry over HTTP, and neither should
// know which one it is.
type Binder interface {
	PartyForSubject(ctx context.Context, subject string) (string, error)
}

// BinderFunc adapts a function to a Binder.
type BinderFunc func(ctx context.Context, subject string) (string, error)

// PartyForSubject implements Binder.
func (f BinderFunc) PartyForSubject(ctx context.Context, subject string) (string, error) {
	return f(ctx, subject)
}

// Middleware verifies the bearer token on a request, if there is one.
//
// Three outcomes, and the middle one is the point:
//
//   - No Authorization header: the request continues with no Caller. That is
//     not permission to do anything; it is the absence of a claim, and a
//     handler that acts in somebody's name gets ErrNoCaller from Acting. Public
//     reads — a verifier checking a credential, the deployment's own
//     self-description — genuinely need this.
//   - A token that does not verify: 401, immediately, before any handler runs.
//     A bad token is an assertion that failed, which is different from making
//     no assertion.
//   - A token that verifies: the Caller is put on the context, with the Party
//     it is bound to already resolved.

// Forget drops one subject from the binding cache. The parties service calls
// it when a binding lands: the request that binds a subject has itself just
// primed the cache with "nobody" — the middleware looked the subject up on the
// way in — and a freshly enrolled worker held to a cached "you do not exist"
// until the TTL passes is the first-login failure the cache must not create.
type Forget func(subject string)

// Middleware verifies bearer tokens and resolves callers. The returned Forget
// invalidates one subject's cache entry; services that never bind ignore it.
func Middleware(v TokenVerifier, binder Binder, clk clock.Clock, log *slog.Logger) (func(http.Handler) http.Handler, Forget) {
	cache := &bindingCache{ttl: time.Minute, clk: clk, entries: map[string]bindingEntry{}}
	forget := func(subject string) {
		cache.mu.Lock()
		delete(cache.entries, subject)
		cache.mu.Unlock()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if internal(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			raw := r.Header.Get("Authorization")
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}
			token, ok := BearerToken(raw)
			if !ok {
				deny(w, http.StatusUnauthorized, "invalid_token",
					"the Authorization header is not a bearer token")
				return
			}
			caller, err := v.Verify(r.Context(), token)
			if err != nil {
				// The reason goes to the log, not to the response. See
				// ErrInvalidToken for why.
				log.Info("token rejected", "path", r.URL.Path, "error", err)
				deny(w, http.StatusUnauthorized, "invalid_token", "the bearer token was not accepted")
				return
			}

			if binder != nil {
				party, err := cache.get(r.Context(), binder, caller.Subject)
				if err != nil {
					// An unreachable registry is an outage, and it must look
					// like one. Reporting it as "you are nobody" would tell a
					// worker their identity had been rejected when in fact
					// nothing about them was ever read.
					log.Error("could not resolve the caller's party", "error", err)
					deny(w, http.StatusServiceUnavailable, "identity_unavailable",
						"the caller could not be resolved to a party; this is an outage, not a rejection")
					return
				}
				caller.PartyID = party
			}
			caller.requestedFor = strings.TrimSpace(r.Header.Get(HeaderOnBehalfOf))

			next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), caller)))
		})
	}, forget
}

// internal is the paths the middleware does not touch: health, readiness, and
// the harness clock. The clock is already refused in production outright
// (pkg/service.chooseClock), which is a stronger statement than requiring a
// token for it.
func internal(path string) bool {
	return path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/internal/")
}

func deny(w http.ResponseWriter, code int, errCode, detail string) {
	if code == http.StatusUnauthorized {
		// Saying which scheme is expected is what lets a client retry
		// correctly rather than guess.
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errCode, "detail": detail})
}

type bindingEntry struct {
	party string
	at    time.Time
}

// bindingCache keeps subject-to-party for a minute.
//
// A minute rather than longer because the binding changes at exactly the
// moment somebody is enrolled, and a worker who has just been registered
// waiting out a long cache is a worker being told they do not exist. Short
// enough not to matter, long enough that a burst of requests from one device
// is one registry call.
type bindingCache struct {
	ttl     time.Duration
	clk     clock.Clock
	mu      sync.Mutex
	entries map[string]bindingEntry
}

func (c *bindingCache) get(ctx context.Context, b Binder, subject string) (string, error) {
	now := c.clk.Now()
	c.mu.Lock()
	e, ok := c.entries[subject]
	c.mu.Unlock()
	if ok && now.Sub(e.at) < c.ttl {
		return e.party, nil
	}

	party, err := b.PartyForSubject(ctx, subject)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	// An unbound subject is cached too. Without that, every request from
	// somebody authenticated but not yet enrolled is a registry lookup, and
	// those are the requests an unenrolled device makes in a loop.
	c.entries[subject] = bindingEntry{party: party, at: now}
	c.mu.Unlock()
	return party, nil
}
