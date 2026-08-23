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

	clk := clock.System{}
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

		if opts.Deliver != nil {
			relay := store.NewRelay(db, opts.Deliver(d), log, clk, time.Second)
			go relay.Run(ctx)
		}
	}

	mux := http.NewServeMux()
	if opts.Routes != nil {
		opts.Routes(mux, d)
	}

	if err := httpx.New(name, cfg.Addr, mux, clk, log, ready).Run(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func newLogger(level, service string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})).
		With("service", service)
}
