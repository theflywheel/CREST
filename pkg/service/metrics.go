package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/theflywheel/crest/pkg/store"
)

type metricsMember struct {
	name string
	db   interface {
		OutboxStats(context.Context) (store.OutboxStats, error)
	}
}

// outboxMetricsHandler serves a deliberately small Prometheus exposition.
// It is registered only below /internal/, where Compose's service-auth
// middleware protects it. Payloads, topics, identities and error text never
// enter the response.
func outboxMetricsHandler(members []metricsMember) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		stats := make([]store.OutboxStats, len(members))
		for i, member := range members {
			if member.db == nil {
				http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
				return
			}
			memberCtx, memberCancel := context.WithTimeout(ctx, time.Second)
			value, err := member.db.OutboxStats(memberCtx)
			memberCancel()
			if err != nil {
				http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
				return
			}
			stats[i] = value
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = fmt.Fprintln(w, "# HELP crest_outbox_pending Undelivered outbox messages not yet claimed.")
		_, _ = fmt.Fprintln(w, "# TYPE crest_outbox_pending gauge")
		_, _ = fmt.Fprintln(w, "# HELP crest_outbox_retrying Undelivered outbox messages claimed at least once.")
		_, _ = fmt.Fprintln(w, "# TYPE crest_outbox_retrying gauge")
		_, _ = fmt.Fprintln(w, "# HELP crest_outbox_oldest_age_seconds Age of the oldest undelivered outbox message.")
		_, _ = fmt.Fprintln(w, "# TYPE crest_outbox_oldest_age_seconds gauge")
		for i, member := range members {
			label := strconv.Quote(member.name)
			_, _ = fmt.Fprintf(w, "crest_outbox_pending{member=%s} %d\n", label, stats[i].Pending)
			_, _ = fmt.Fprintf(w, "crest_outbox_retrying{member=%s} %d\n", label, stats[i].Retrying)
			_, _ = fmt.Fprintf(w, "crest_outbox_oldest_age_seconds{member=%s} %g\n", label, stats[i].OldestAgeSecs)
		}
	}
}
