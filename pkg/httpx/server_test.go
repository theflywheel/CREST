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

	"github.com/theflywheel/crest/pkg/httpx"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return newTestServerReady(t, nil)
}

func newTestServerReady(t *testing.T, ready httpx.ReadyFunc) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpx.New("test", ":0", http.NewServeMux(), log, ready).Handler()
}

// The harness polls readiness instead of sleeping, so these endpoints are load
// bearing: if they lie, every E2E test becomes flaky.
func TestHealthEndpointsReport(t *testing.T) {
	h := newTestServer(t)

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

// Health must report this process's real time in UTC. The harness compares
// core's health time against payments' to catch a stack whose two processes
// disagree about when it is, and a health time that was anything other than
// real time would make that check meaningless.
func TestHealthReportsRealUTCTime(t *testing.T) {
	before := time.Now().UTC().Add(-time.Second)
	h := newTestServer(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Time.Before(before) || body.Time.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("health time = %v, want a real instant near now", body.Time)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	h := newTestServer(t)
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
	h := newTestServerReady(t, func(context.Context) error {
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
	h := newTestServerReady(t, func(context.Context) error {
		return errors.New("down")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz = %d, want 200", rec.Code)
	}
}
