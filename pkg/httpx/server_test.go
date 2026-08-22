package httpx_test

import (
	"encoding/json"
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpx.New("test", ":0", http.NewServeMux(), clk, log).Handler()
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
