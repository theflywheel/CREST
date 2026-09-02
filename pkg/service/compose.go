package service

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/identity"
	"github.com/theflywheel/crest/pkg/store"
)

// Member is one service running inside a composed process (#150). Its name is
// its schema: composing changed the deployment shape, not the boundaries, and
// each member keeps its own tables, migrations, outbox and route family.
type Member struct {
	Name string
	Opts Options
}

// Compose runs several services as one process on one port (#150).
//
// It is Main for a list: every member gets the same per-service setup —
// schema migration, registry substrate, object store, outbox relay — and all
// of their routes land on one mux, where a path collision panics at startup
// rather than shadowing a handler quietly.
//
// Identity is process-wide. The one member that defines a Binder (parties)
// is the identity authority, and its local binder, permits and same-party
// answers serve every member — in-process instead of the HTTP round trip the
// members made as separate deployables. With no authority member the remote
// paths stand, which is what keeps Main a one-member Compose.
func Compose(name string, members []Member) {
	cfg, err := config.LoadBase(name)
	log := newLogger(cfg.LogLevel, name)
	if err != nil {
		log.Error("configuration invalid", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clk, driveable := chooseClock(cfg, log)

	deps := make([]Deps, len(members))
	var pings []httpx.ReadyFunc
	for i, m := range members {
		d := Deps{Config: cfg, Log: log.With("member", m.Name), Clock: clk, Ctx: ctx}
		if len(members) == 1 {
			d.Log = log
		}
		if m.Opts.Migrations != nil {
			db, err := store.Open(ctx, cfg.DatabaseURL, m.Name, clk)
			if err != nil {
				log.Error("database unavailable", "member", m.Name, "error", err)
				os.Exit(1)
			}
			defer db.Close()

			if m.Opts.FormerName != "" {
				if err := db.AdoptLegacySchema(ctx, m.Opts.FormerName); err != nil {
					log.Error("could not adopt the former schema",
						"member", m.Name, "former", m.Opts.FormerName, "error", err)
					os.Exit(1)
				}
			}

			// Fatal on purpose. A service that starts with its schema
			// half-applied answers requests it cannot honour, and the failure
			// surfaces later as a missing column in a payment path.
			if err := db.Migrate(ctx, m.Opts.Migrations, m.Opts.Dir); err != nil {
				log.Error("migrations failed", "member", m.Name, "error", err)
				os.Exit(1)
			}
			log.Info("schema up to date", "schema", db.Schema())
			d.DB = db
			pings = append(pings, db.Ping)

			if len(m.Opts.DeDiRegistries) > 0 {
				dcfg := dedi.LoadConfig()
				pub, err := dedi.New(dcfg, db, clk, log)
				if err != nil {
					log.Error("registry substrate unusable", "member", m.Name, "error", err)
					os.Exit(1)
				}
				// Bootstrap is fatal on failure for the same reason a
				// migration is: a service that starts without its registries
				// answers writes it cannot publish.
				for _, reg := range m.Opts.DeDiRegistries {
					if err := pub.EnsureRegistry(ctx, dcfg.Namespace, reg,
						"CREST public facts — Blueprint §3"); err != nil {
						log.Error("could not create registry", "registry", reg, "error", err)
						os.Exit(1)
					}
				}
				d.DeDi, d.DeDiNamespace = pub, dcfg.Namespace
			}

			if m.Opts.NeedsBlobs {
				s3cfg, ok := store.LoadS3Config()
				if !ok {
					log.Error("this service stores artefacts and S3_ENDPOINT is not set",
						"member", m.Name)
					os.Exit(1)
				}
				blobs, err := store.NewS3(s3cfg)
				if err != nil {
					log.Error("object store unusable", "error", err)
					os.Exit(1)
				}
				if err := blobs.EnsureBucket(ctx); err != nil {
					log.Error("object store unreachable", "error", err)
					os.Exit(1)
				}
				log.Info("object store ready", "bucket", s3cfg.Bucket)
				d.Blobs = blobs
			}
		}
		deps[i] = d
	}

	// One member may be the identity authority (Binder set); its answers
	// serve the whole process. Two claiming it is a wiring error, not a
	// tiebreak.
	authority := -1
	for i, m := range members {
		if m.Opts.Binder != nil {
			if authority >= 0 {
				log.Error("two members define a Binder; one process has one identity authority",
					"first", members[authority].Name, "second", m.Name)
				os.Exit(1)
			}
			authority = i
		}
	}

	idCfg, haveIdentity, err := identity.LoadConfig()
	if err != nil {
		log.Error("identity provider configuration is unusable", "error", err)
		os.Exit(1)
	}
	if why := startupRefusal(cfg.Env, haveIdentity); why != "" {
		log.Error("refusing to start", "why", why,
			"set", "CREST_OIDC_ISSUER, CREST_OIDC_JWKS_URL, CREST_SUBJECT_SALT")
		os.Exit(1)
	}

	sameParty := remoteSameParty(config.Str("PARTIES_URL", ""))
	if authority >= 0 && members[authority].Opts.SameParty != nil {
		sameParty = members[authority].Opts.SameParty(deps[authority])
	}
	permits := refuseEverything
	if haveIdentity {
		if authority >= 0 && members[authority].Opts.Permits != nil {
			permits = members[authority].Opts.Permits(deps[authority])
		} else {
			permits = remotePermits(config.Str("PARTIES_URL", ""))
		}
	}

	var mw []httpx.Middleware
	// CORS is outermost so a browser's preflight is answered before identity
	// looks for a token the preflight never carries. Off unless the
	// deployment names its web origins.
	if origins := config.Str("CREST_CORS_ORIGINS", ""); origins != "" {
		mw = append(mw, httpx.CORSFromOrigins(origins))
	}

	// Identity is wired BEFORE routes so the handlers' copy of Deps carries
	// ForgetSubject — a handler holding a Deps snapshot from before this
	// point would silently never invalidate the binding cache, which is
	// exactly the first-login bug Forget exists to prevent.
	var forget identity.Forget
	if haveIdentity {
		binder := identity.RemoteBinder(config.Str("PARTIES_URL", ""))
		if authority >= 0 {
			binder = members[authority].Opts.Binder(deps[authority])
		}
		var m httpx.Middleware
		m, forget = identity.Middleware(identity.NewMultiVerifier(idCfg), binder, clk, log)
		mw = append(mw, m)
		log.Info("callers are authenticated", "issuer", idCfg.Issuer, "jwks", idCfg.JWKSURL)
	} else {
		// Loud on purpose. This is the state the whole package exists to end,
		// and a service that entered it silently would look identical in a
		// log to one that did not.
		log.Warn("no identity provider is configured: callers are not authenticated",
			"consequence", "an endpoint that acts in somebody's name will refuse rather than guess")
	}

	for i := range deps {
		deps[i].SameParty = sameParty
		deps[i].Permits = permits
		deps[i].Authenticating = haveIdentity
		deps[i].ForgetSubject = forget
	}

	for i, m := range members {
		if m.Opts.Deliver != nil && deps[i].DB != nil {
			relay := store.NewRelay(deps[i].DB, m.Opts.Deliver(deps[i]), deps[i].Log, clk, time.Second)
			go relay.Run(ctx)
		}
		if m.Opts.OnStart != nil {
			if err := m.Opts.OnStart(ctx, deps[i]); err != nil {
				log.Error("start-up task failed", "member", m.Name, "error", err)
				os.Exit(1)
			}
		}
	}

	mux := http.NewServeMux()
	for i, m := range members {
		if m.Opts.Routes != nil {
			m.Opts.Routes(mux, deps[i])
		}
	}
	if driveable != nil {
		registerClockControl(mux, driveable, log)
	}

	// Ready means every member's schema answers: one member's dead store
	// must fail the whole process's readiness rather than hide behind a
	// sibling's healthy ping.
	var ready httpx.ReadyFunc
	if len(pings) > 0 {
		all := pings
		ready = func(ctx context.Context) error {
			for _, p := range all {
				if err := p(ctx); err != nil {
					return err
				}
			}
			return nil
		}
	}

	if err := httpx.New(name, cfg.Addr, mux, clk, log, ready, mw...).Run(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
