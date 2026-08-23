package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/httpx"
)

func newTestServer(t *testing.T, clk clock.Clock) http.Handler {
	t.Helper()
	return newTestServerReady(t, clk, nil)
}

func newTestServerReady(t *testing.T, clk clock.Clock, ready httpx.ReadyFunc) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpx.New("test", ":0", http.NewServeMux(), clk, log, ready).Handler()
}

// The harness polls readiness instead of sleeping, so these endpoints are load
// bearing: if they lie, every E2E test becomes flaky.
func TestHealthEndpointsReport(t *testing.T) {
	epoch := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	h := newTestServer(t, clock.NewFake(epoch))

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s body is not JSON: %v", path, err)
		}
		if body["service"] != "test" {
			t.Fatalf("%s service = %v, want test", path, body["service"])
		}
	}
}

// Health must report the injected clock, not wall time — otherwise a service
// silently reads real time and the seven-day window becomes untestable.
func TestHealthUsesInjectedClock(t *testing.T) {
	epoch := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	h := newTestServer(t, clock.NewFake(epoch))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if !body.Time.Equal(epoch) {
		t.Fatalf("health time = %v, want the injected %v", body.Time, epoch)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h := newTestServer(t, clock.System{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// A readiness check that fails must make the endpoint fail. The harness polls
// /readyz instead of sleeping, so a readyz that always says yes turns every
// start-up race into a flaky test rather than a failed one.
func TestReadinessReportsItsDependency(t *testing.T) {
	h := newTestServerReady(t, clock.NewFake(time.Now()), func(context.Context) error {
		return errors.New("the database is not accepting connections")
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz = %d while its dependency was down, want 503", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["reason"] == nil {
		t.Error("readyz said no without saying why")
	}
}

// Liveness is not readiness: a service whose database is down is still alive,
// and conflating the two makes an orchestrator restart a healthy process.
func TestHealthStaysUpWhenTheDependencyIsDown(t *testing.T) {
	h := newTestServerReady(t, clock.NewFake(time.Now()), func(context.Context) error {
		return errors.New("down")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz = %d, want 200", rec.Code)
	}
}
