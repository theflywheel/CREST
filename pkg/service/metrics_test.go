package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theflywheel/crest/pkg/store"
)

type fakeMetricsDB struct {
	stats store.OutboxStats
	err   error
	calls int
}

func (f *fakeMetricsDB) OutboxStats(ctx context.Context) (store.OutboxStats, error) {
	f.calls++
	if _, ok := ctx.Deadline(); !ok {
		return store.OutboxStats{}, errors.New("missing query deadline")
	}
	return f.stats, f.err
}

func TestOutboxMetricsPrometheus(t *testing.T) {
	parties := &fakeMetricsDB{stats: store.OutboxStats{Pending: 2, Retrying: 1, OldestAgeSecs: 12.5}}
	payments := &fakeMetricsDB{stats: store.OutboxStats{Pending: 0, Retrying: 3, OldestAgeSecs: 4}}
	h := outboxMetricsHandler([]metricsMember{
		{name: "parties", db: parties},
		{name: "payments", db: payments},
	})
	r := httptest.NewRequest(http.MethodGet, "/internal/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("content type = %q, want Prometheus text", got)
	}
	body := w.Body.String()
	for _, want := range []string{
		"# TYPE crest_outbox_pending gauge",
		`crest_outbox_pending{member="parties"} 2`,
		`crest_outbox_retrying{member="parties"} 1`,
		`crest_outbox_oldest_age_seconds{member="parties"} 12.5`,
		`crest_outbox_retrying{member="payments"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"payload", "topic", "subject", "email", "secret"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("metrics body contains forbidden field %q:\n%s", forbidden, body)
		}
	}
	if parties.calls != 1 || payments.calls != 1 {
		t.Fatalf("calls = parties %d, payments %d, want one each", parties.calls, payments.calls)
	}
}

func TestOutboxMetricsUnavailableIs503(t *testing.T) {
	h := outboxMetricsHandler([]metricsMember{
		{name: "parties", db: &fakeMetricsDB{err: errors.New("database unavailable")}},
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/metrics", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "crest_outbox_") {
		t.Fatalf("unavailable response advertised metrics: %s", w.Body.String())
	}
}

func TestOutboxMetricsNilDatabaseIs503(t *testing.T) {
	h := outboxMetricsHandler([]metricsMember{{name: "parties"}})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/metrics", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
