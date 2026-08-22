// Package httpx holds the HTTP scaffolding every CREST service shares:
// health endpoints, graceful shutdown, request logging.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
)

// Server wraps http.Server with the lifecycle CREST services need.
type Server struct {
	name string
	log  *slog.Logger
	srv  *http.Server
}

// New builds a server for a service. The mux is the service's own routes;
// health endpoints are added here so every service reports readiness the same
// way — the harness polls these instead of sleeping.
func New(name, addr string, mux *http.ServeMux, clk clock.Clock, log *slog.Logger) *Server {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": name,
			"status":  "ok",
			"time":    clk.Now(),
		})
	})
	// readyz is separate on purpose: a service can be alive but not yet able to
	// serve (migrations pending, dependency unreachable). Conflating them makes
	// the harness wait on the wrong signal.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"service": name, "status": "ready"})
	})

	return &Server{
		name: name,
		log:  log,
		srv: &http.Server{
			Addr:              addr,
			Handler:           logging(log, mux),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       2 * time.Minute,
		},
	}
}

// Run serves until SIGINT/SIGTERM, then drains in-flight requests.
func (s *Server) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "service", s.name, "addr", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down", "service", s.name)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	}
}

// Handler exposes the wrapped handler so tests can drive the service without a port.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health checks are polled constantly by the harness; logging them buries
		// everything that matters.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
