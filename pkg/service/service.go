// Package service is the common bootstrap for a CREST service.
//
// Every service starts identically: read config, build a logger, take a clock,
// register routes, serve, shut down cleanly. Keeping that in one place means a
// change to how services behave is one edit, not seven.
package service

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
)

// Deps is what a service's route registration is handed.
type Deps struct {
	Config config.Base
	Log    *slog.Logger
	Clock  clock.Clock
}

// Routes registers a service's own endpoints. Health endpoints are added by httpx.
type Routes func(mux *http.ServeMux, d Deps)

// Main is the entire main() of a CREST service.
func Main(name string, routes Routes) {
	cfg, err := config.LoadBase(name)
	log := newLogger(cfg.LogLevel, name)
	if err != nil {
		log.Error("configuration invalid", "error", err)
		os.Exit(1)
	}

	d := Deps{Config: cfg, Log: log, Clock: clock.System{}}
	mux := http.NewServeMux()
	routes(mux, d)

	if err := httpx.New(name, cfg.Addr, mux, d.Clock, log).Run(); err != nil {
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
