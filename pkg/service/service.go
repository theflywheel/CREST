// Package service is the common bootstrap for a CREST service.
//
// Every service starts identically: read config, build a logger, take a clock,
// open the database, migrate its own schema, register routes, drain its outbox,
// serve, shut down cleanly. Keeping that in one place means a change to how
// services behave is one edit rather than seven — and it means no service can
// quietly skip the migration or the outbox relay.
package service

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"

	"github.com/theflywheel/crest/pkg/client"
	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

// Deps is what a service's route registration is handed.
type Deps struct {
	Config config.Base
	Log    *slog.Logger
	Clock  clock.Clock

	// Ctx is the process lifetime, cancelled when the service is shutting
	// down. It is here so a service can start its own background work from
	// Routes — the confirmation sweep is the one that matters — and have that
	// work stop with the process rather than outlive it.
	Ctx context.Context

	// DB is nil for a service that declares no migrations. A service with no
	// database is a legitimate thing; a service that
	// silently got a nil DB because its migrations failed is not, which is why
	// a migration error is fatal rather than logged.
	DB *store.DB

	// DeDi is the registry substrate, present only for a service that asked
	// for one. It is an interface rather than a client because a deployment
	// may be running without a transparency log behind it (#20), and the
	// difference is visible through DeDi.Transparent() rather than hidden.
	DeDi dedi.Publisher

	// DeDiNamespace is the namespace this deployment writes its public facts
	// under. Kept beside the publisher so no service has to read the
	// environment a second time and get a different answer.
	DeDiNamespace string

	// Permits answers the registry's authorization question — "may this party
	// perform this function here" — from wherever this service can reach it.
	// Handed down rather than built per service so that no service invents a
	// second authorization model beside the one §4 describes.
	//
	// Never nil: with no identity provider configured it refuses everything,
	// which is the safe reading of "we cannot check".
	Permits identity.PermitsFunc

	// ForgetSubject invalidates the identity middleware's binding cache for
	// one subject. Non-nil only when identity is configured; the parties
	// service calls it when a binding lands, so a first login is not held to
	// the cached "nobody" its own bind request primed.
	ForgetSubject identity.Forget

	// Authenticating is whether this deployment has an identity provider. It
	// is what handlers pass to identity.Actor: true means an endpoint acting
	// in somebody's name refuses an unauthenticated request rather than
	// believing the party id it was handed.
	//
	// Always true in production — Main refuses to start otherwise.
	Authenticating bool

	// SameParty answers which party ids are one person, after any merge
	// (#100). Survivor first.
	//
	// Every read that filters by party goes through it. A merge deliberately
	// rewrites nothing — the claims, windows and instructions already written
	// still name the party they were recorded against, because that was a true
	// statement at the time — so a read that asked about one id only would
	// find a hole exactly where the system corrected itself about who somebody
	// was.
	//
	// Never nil. With no registry reachable it returns the id it was given,
	// which is the pre-merge behaviour and is stated rather than silent: see
	// remoteSameParty.
	SameParty SamePartyFunc

	// Blobs is the object store, present only for a service that asked for
	// one. What lives in it is consent artefacts — the voice recording that
	// is a non-literate worker's only real way to consent (§9) — so it is
	// deliberately not handed to every service by default.
	Blobs store.Blobs
}

// Routes registers a service's own endpoints. Health endpoints are added by httpx.
type Routes func(mux *http.ServeMux, d Deps)

// Options is how a service says what it needs.
type Options struct {
	// Migrations is the embedded FS holding the service's .sql files, and Dir
	// is the path inside it. The service owns exactly one schema, named after
	// itself.
	Migrations fs.FS
	Dir        string

	// FormerName is what this service — and therefore its schema — used to be
	// called. On startup, a database holding the old schema and not the new
	// one has it renamed in place, so a deployment crosses a service rename
	// (#50: registry → parties) without a data migration anybody has to
	// remember to run.
	FormerName string

	// Deliver builds the function that drains this service's outbox. It is a
	// factory rather than the function itself because delivering a message
	// usually means recording what happened — a payment sent, a notification
	// delivered — and that needs the same database handle the rest of the
	// service uses.
	//
	// Nil means the service never enqueues anything.
	Deliver func(d Deps) store.Deliverer

	// DeDiRegistries names the registries this service publishes public facts
	// into (Blueprint §3). Non-empty selects a registry substrate, builds it
	// from the environment, and makes each registry exist before the service
	// answers a request.
	//
	// A service that lists none gets a nil Deps.DeDi. That is the honest
	// default: most services hold personal data, and personal data never
	// reaches the node.
	DeDiRegistries []string

	// NeedsBlobs asks for an object store. Like the registry substrate, a
	// missing configuration is fatal rather than a silent downgrade: a service
	// that needs somewhere to put a consent recording and quietly starts
	// without one will accept a consent it cannot evidence.
	NeedsBlobs bool

	// OnStart runs once, after migrations and after the registry substrate is
	// ready, before the service answers anything.
	//
	// It exists for bootstrap that belongs to one service rather than to all of
	// them — publishing the deployment's own self-description, for instance.
	// A failure here is fatal for the same reason a migration failure is: a
	// service that started without its bootstrap answers requests it cannot
	// honour, and the gap surfaces much later as something missing.
	OnStart func(ctx context.Context, d Deps) error

	// Binder resolves a verified subject to a Party. Only the registry sets
	// it — it owns the table — and everybody else gets a client that asks the
	// registry.
	Binder func(d Deps) identity.Binder

	// Permits overrides how this service checks an authorization. Same
	// reasoning as Binder: the registry answers it locally, the others ask.
	Permits func(d Deps) identity.PermitsFunc

	// SameParty overrides how this service expands a party across merges.
	// Only the registry sets it; it owns the parties table.
	SameParty func(d Deps) SamePartyFunc

	// ClockSeam lets a service ask for a clock the harness can drive, and
	// mount the route that drives it. Leave it nil — almost everything should
	// — and the service reads wall-clock time and has no /internal/clock at
	// all, not even one that refuses.
	//
	// It is a hook rather than a setting because the only reason a CREST
	// process ever wants driveable time is the confirmation window, and the
	// confirmation window is programme policy of the payments application,
	// not an infrastructure primitive (ruled 2026-08-28, #127). pkg/service
	// used to give every service the capability outright; now each mount is a
	// decision written in that service's own wiring. pkg/clockctl.Seam is the
	// implementation, and it is the payments application's.
	ClockSeam ClockSeamFunc

	// Metrics contributes this member's own counters to GET
	// /internal/metrics, beside the outbox gauges every member already
	// publishes.
	//
	// It exists because a fact worth alerting on has to be readable from
	// outside the process. The first one is the core↔payments clock skew the
	// confirmation window's opening instant depends on (#221): payments logs
	// a warning when the instant evidence supplied and its own arrival clock
	// disagree, and a log line nobody greps is not a detector.
	//
	// Called on every scrape, so it must be cheap and must not touch the
	// database — the outbox gauges are the only thing here allowed a query.
	Metrics func() []Metric

	Routes Routes
}

// Metric is one number a member publishes on /internal/metrics.
//
// Deliberately small: a name, a help line, a type and a value. Nothing here
// carries a payload, a topic or an identity, for the reason metrics.go gives —
// this surface is protected by service auth, not by hoping nobody looks.
type Metric struct {
	Name  string
	Help  string
	Type  string // "counter" | "gauge"
	Value float64
}

// ClockSeamFunc chooses the process clock and, when it is driveable, returns
// the mount that exposes it. A nil second result means there is nothing to
// mount. See pkg/clockctl.
type ClockSeamFunc func(cfg config.Base, log *slog.Logger) (clock.Clock, func(*http.ServeMux))

// sameSeam reports whether two members named the same seam function.
//
// Compared by code pointer, which is exactly the question being asked: several
// members of one process may declare the driveable clock (#215), and that is
// fine as long as they all mean pkg/clockctl.Seam. Two DIFFERENT seams would
// make the process's clock depend on member order, which is a wiring mistake
// worth refusing to start over.
func sameSeam(a, b ClockSeamFunc) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// Main is the entire main() of a CREST service.
func Main(name string, opts Options) {
	// One member, same machinery: Compose is Main for a list, and keeping a
	// single path is what stops the two from drifting apart.
	Compose(name, []Member{{Name: name, Opts: opts}})
}

func newLogger(level, service string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})).
		With("service", service)
}

// refuseEverything is the authorization check a service gets when it has no
// identity provider to check against.
//
// It says no. The alternative — saying yes because there is nothing to ask —
// is how an unconfigured deployment ends up more permissive than a configured
// one, which is exactly backwards.
func refuseEverything(context.Context, string, string, string) (bool, error) {
	return false, nil
}

// remotePermits asks the registry the authorization question over HTTP.
func remotePermits(registryBase string) identity.PermitsFunc {
	c := client.New(registryBase)
	return func(ctx context.Context, partyID, function, contextID string) (bool, error) {
		q := url.Values{"partyId": {partyID}, "function": {function}}
		if contextID != "" {
			q.Set("contextId", contextID)
		}
		// `permitted`, which is what the registry actually answers. A field
		// name that does not match decodes to false and the check then fails
		// closed — safe, and invisible until somebody legitimate is refused.
		var out struct {
			Permitted bool `json:"permitted"`
		}
		if err := c.Get(ctx, "/internal/authorizations/permits?"+q.Encode(), &out); err != nil {
			return false, err
		}
		return out.Permitted, nil
	}
}

// startupRefusal names why this service must not start, or returns "".
//
// Separated from Main so the rule can be tested without a process that exits.
// The rule is the part worth testing: a production deployment with no identity
// provider is one where any client that can reach a port can withdraw a
// worker's enrolment consent (#89), and the symptom is a worker whose work
// quietly stopped counting with nothing anywhere saying who did it.
//
// Local and staging may run without one. They say so loudly, and their
// endpoints that act in somebody's name refuse rather than guess.
func startupRefusal(env string, haveIdentity bool) string {
	if env == "production" && !haveIdentity {
		return "no identity provider is configured, so every caller would be whoever they say they are"
	}
	return ""
}

// SamePartyFunc answers which party ids are one person, survivor first.
type SamePartyFunc func(ctx context.Context, partyID string) ([]string, error)

// remoteSameParty asks the registry which ids are one person.
func remoteSameParty(registryBase string) SamePartyFunc {
	c := client.New(registryBase)
	return func(ctx context.Context, partyID string) ([]string, error) {
		if partyID == "" {
			return nil, nil
		}
		var out struct {
			Identifiers []string `json:"identifiers"`
		}
		err := c.Get(ctx, "/internal/parties/"+url.PathEscape(partyID)+"/identifiers", &out)
		if err != nil {
			// A party the registry has never heard of is not an error here.
			// Services hold records for parties that were deleted or that
			// arrived through a fixture, and answering "just this one" is the
			// same answer as before merges existed.
			if client.Code(err) == 404 {
				return []string{partyID}, nil
			}
			// Anything else is an outage, and it is reported rather than
			// swallowed. Falling back to the single id would return a history
			// with a hole in it and no indication that anything was missing —
			// which is worse than a failed read, because the caller would
			// believe it.
			return nil, fmt.Errorf("registry could not say which ids are %s: %w", partyID, err)
		}
		if len(out.Identifiers) == 0 {
			return []string{partyID}, nil
		}
		return out.Identifiers, nil
	}
}
