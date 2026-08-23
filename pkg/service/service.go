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
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/dedi"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/store"
)

// Deps is what a service's route registration is handed.
type Deps struct {
	Config config.Base
	Log    *slog.Logger
	Clock  clock.Clock

	// DB is nil for a service that declares no migrations. A service with no
	// database is a legitimate thing (notify is nearly one); a service that
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

	Routes Routes
}

// Main is the entire main() of a CREST service.
func Main(name string, opts Options) {
	cfg, err := config.LoadBase(name)
	log := newLogger(cfg.LogLevel, name)
	if err != nil {
		log.Error("configuration invalid", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clk, driveable := chooseClock(cfg, log)
	d := Deps{Config: cfg, Log: log, Clock: clk}

	var ready httpx.ReadyFunc
	if opts.Migrations != nil {
		db, err := store.Open(ctx, cfg.DatabaseURL, name, clk)
		if err != nil {
			log.Error("database unavailable", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		// Fatal on purpose. A service that starts with its schema half-applied
		// answers requests it cannot honour, and the failure surfaces later as
		// a missing column in a payment path.
		if err := db.Migrate(ctx, opts.Migrations, opts.Dir); err != nil {
			log.Error("migrations failed", "error", err)
			os.Exit(1)
		}
		log.Info("schema up to date", "schema", db.Schema())
		d.DB = db
		ready = db.Ping

		if len(opts.DeDiRegistries) > 0 {
			cfg := dedi.LoadConfig()
			pub, err := dedi.New(cfg, db, clk, log)
			if err != nil {
				log.Error("registry substrate unusable", "error", err)
				os.Exit(1)
			}
			// Bootstrap is fatal on failure for the same reason a migration
			// is. A service that starts without its registries answers writes
			// it cannot publish, and the outbox then retries them forever
			// against a namespace that does not exist.
			for _, reg := range opts.DeDiRegistries {
				if err := pub.EnsureRegistry(ctx, cfg.Namespace, reg,
					"CREST public facts — Blueprint §3"); err != nil {
					log.Error("could not create registry", "registry", reg, "error", err)
					os.Exit(1)
				}
			}
			d.DeDi, d.DeDiNamespace = pub, cfg.Namespace
		}

		if opts.Deliver != nil {
			relay := store.NewRelay(db, opts.Deliver(d), log, clk, time.Second)
			go relay.Run(ctx)
		}
	}

	mux := http.NewServeMux()
	if opts.Routes != nil {
		opts.Routes(mux, d)
	}
	if driveable != nil {
		registerClockControl(mux, driveable, log)
	}

	if err := httpx.New(name, cfg.Addr, mux, clk, log, ready).Run(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// chooseClock decides whether this process reads wall-clock time or is driven.
//
// The confirmation window is seven days. A harness that waits seven days is not
// a harness, and one that shortens the window to a second is testing a
// different system — the whole point of W3 is what happens at the boundary of a
// real week. So outside production a service can be handed its time, and the
// harness advances it.
//
// Refused in production, loudly. A running deployment whose clock an HTTP call
// can move is a deployment where a confirmation window can be closed early on
// someone, and that is a way to take a worker's chance to object away from them.
func chooseClock(cfg config.Base, log *slog.Logger) (clock.Clock, *clock.Fake) {
	if !config.MustBool("CLOCK_DRIVEABLE", false) {
		return clock.System{}, nil
	}
	if cfg.Env == "production" {
		log.Error("CLOCK_DRIVEABLE is set in production; refusing to start",
			"why", "a clock an HTTP call can move can close a worker's confirmation window early")
		os.Exit(1)
	}
	start := clock.System{}.Now()
	if s := config.Str("CLOCK_START", ""); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			log.Error("CLOCK_START is not RFC3339", "value", s, "error", err)
			os.Exit(1)
		}
		start = t
	}
	fake := clock.NewFake(start)
	log.Warn("clock is driveable", "now", start, "env", cfg.Env)
	return fake, fake
}

// registerClockControl exposes the clock under /internal/, which is the prefix
// for everything that exists for the harness rather than for a caller.
func registerClockControl(mux *http.ServeMux, fake *clock.Fake, log *slog.Logger) {
	mux.HandleFunc("GET /internal/clock", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"now": fake.Now()})
	})
	mux.HandleFunc("POST /internal/clock", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Now     *time.Time `json:"now,omitempty"`
			Advance string     `json:"advance,omitempty"`
		}
		if !httpx.ReadJSON(w, r, &body) {
			return
		}
		switch {
		case body.Now != nil:
			fake.Set(*body.Now)
		case body.Advance != "":
			d, err := time.ParseDuration(body.Advance)
			if err != nil {
				httpx.WriteError(w, http.StatusBadRequest, "invalid_duration",
					"advance is not a duration: %v", err)
				return
			}
			fake.Advance(d)
		default:
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body", "set now, or advance by a duration")
			return
		}
		log.Info("clock moved", "now", fake.Now())
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"now": fake.Now()})
	})
}

func newLogger(level, service string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})).
		With("service", service)
}
