// Package clockctl is the driveable-clock harness surface of the payments
// application.
//
// It used to live in pkg/service, which meant every CREST service — parties,
// definitions, evidence, verification — could have its time moved by an HTTP
// call, because one of them needed it. That was policy leaking into the
// substrate: the only reason a CREST process ever wants a driveable clock is
// the confirmation window, and the confirmation window is programme policy of
// the payments application, not an infrastructure primitive (ruled 2026-08-28,
// #127).
//
// So the seam is opt-in and lives here. A service mounts it by naming it in
// service.Options.ClockSeam; a service that does not name it reads wall-clock
// time and has no /internal/clock route at all — not a route that refuses,
// which is a route somebody can be talked into enabling, but no route.
//
// Mounting it is a decision each service makes in the open, and only two do:
// the payments application, which owns the window, and — for now, against this
// ruling — the core service, because the attestation member still holds the
// window and its sweep (see services/core/attestation/service.go). That second
// mount goes when the member moves.
package clockctl

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/config"
	"github.com/theflywheel/crest/pkg/httpx"
)

// Seam is a service.ClockSeamFunc: it chooses the process clock and, when that
// clock is driveable, returns the mount that exposes it.
//
// The confirmation window is a week long in the CHW programme. A harness that
// waits a week is not a harness, and one that shortens the window to a second
// is testing a different system — the whole point is what happens at the
// boundary of a real week. So outside production a service that opts in can be
// handed its time, and the harness advances it.
//
// Refused in production, loudly. A running deployment whose clock an HTTP call
// can move is a deployment where a confirmation window can be closed early on
// someone, and that is a way to take a worker's chance to object away from
// them.
func Seam(cfg config.Base, log *slog.Logger) (clock.Clock, func(*http.ServeMux)) {
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
	// Offset rather than Fake: driveable, but it still ticks. A frozen clock
	// makes a seeded demo stack look alive while nothing in it can ever become
	// due again — no window ever closes, no payment is released by time
	// passing. See clock.Offset for the whole argument.
	c := clock.NewOffsetAt(clock.System{}, start)
	log.Warn("clock is driveable", "now", start, "skew", c.Skew(), "env", cfg.Env)
	return c, func(mux *http.ServeMux) { Register(mux, c, log) }
}

// Register exposes the clock under /internal/, which is the prefix for
// everything that exists for the harness rather than for a caller.
func Register(mux *http.ServeMux, fake clock.Driveable, log *slog.Logger) {
	mux.HandleFunc("GET /internal/clock", func(w http.ResponseWriter, _ *http.Request) {
		out := map[string]any{"now": fake.Now()}
		// A shifted clock says so, rather than leaving a caller to work out why
		// the stack disagrees with their watch.
		if o, ok := fake.(*clock.Offset); ok {
			out["skew"] = o.Skew().String()
			out["ticking"] = true
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /internal/clock", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Now     *time.Time `json:"now,omitempty"`
			Advance string     `json:"advance,omitempty"`
			// Live puts the clock back on real time. The seeder uses it to
			// hand a stack back after walking a week forward through it.
			Live bool `json:"live,omitempty"`
		}
		if !httpx.ReadJSON(w, r, &body) {
			return
		}
		switch {
		case body.Live:
			fake.Set(clock.System{}.Now())
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
			httpx.WriteError(w, http.StatusBadRequest, "invalid_body",
				"set now, advance by a duration, or go live")
			return
		}
		log.Info("clock moved", "now", fake.Now())
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"now": fake.Now()})
	})
}
